//go:build linux

package listenerevidence

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/processevidence"
	"golang.org/x/sys/unix"
)

const listenerContractHelperEnvironment = "SOLOVEY_LISTENER_CONTRACT_HELPER"

func TestObserveAcceptingTCPBindsRealSocketIdentityWithoutMandatoryGetfd(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)

	observation, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("listener diagnostic: %+v", observation.Diagnostic)
	for _, socket := range observation.Sockets {
		if socket.Network == hostfacts.NetworkTCP && socket.Family == hostfacts.FamilyIPv4 &&
			socket.Bind == "127.0.0.1" && socket.Port == port && socket.Inode != "" && !socket.Wildcard &&
			hostfacts.ValidListenerSocketIdentity(socket) {
			if observation.Diagnostic.ProofMethod == "" || observation.Diagnostic.Stage != "complete" {
				t.Fatalf("listener proof omitted bounded provenance: %#v", observation.Diagnostic)
			}
			return
		}
	}
	t.Fatalf("real accepting socket identity was not observed: port=%d observation=%#v", port, observation)
}

func TestOwnerLocalFaultInjectionPreservesMeaningfulListenerStages(t *testing.T) {
	tests := []struct {
		name       string
		fault      func(*listenerEnvironment)
		reason     Reason
		stage      string
		retryCount int
	}{
		{name: "process fence", reason: ReasonPIDFDAuthority, stage: "pidfd_open", fault: func(environment *listenerEnvironment) {
			environment.pidfdOpen = func(int, int) (int, error) { return -1, unix.EPERM }
		}},
		{name: "process generation before", reason: ReasonProcessChanged, stage: "process_generation_before", retryCount: 2, fault: func(environment *listenerEnvironment) {
			environment.readProcessStart = func(int) (string, error) { return "", unix.ENOENT }
		}},
		{name: "fd snapshot", reason: ReasonDescriptorInventory, stage: "descriptor_inventory", fault: func(environment *listenerEnvironment) {
			environment.readDescriptorSnapshot = func(int) (processevidence.DescriptorSnapshot, error) {
				return processevidence.DescriptorSnapshot{}, listenerError(ReasonDescriptorInventory, injectedDiagnostic("descriptor_inventory", "EACCES"))
			}
		}},
		{name: "socket table open", reason: ReasonSocketTable, stage: "socket_table_open", fault: func(environment *listenerEnvironment) {
			environment.readSocketTables = func(int, hostfacts.Network, map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error) {
				return nil, listenerError(ReasonSocketTable, injectedDiagnostic("socket_table_open", "EACCES"))
			}
		}},
		{name: "socket table read", reason: ReasonSocketTable, stage: "socket_table_read", fault: func(environment *listenerEnvironment) {
			environment.readSocketTables = func(int, hostfacts.Network, map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error) {
				return nil, listenerError(ReasonSocketTable, injectedDiagnostic("socket_table_read", "EIO"))
			}
		}},
		{name: "socket table parse", reason: ReasonSocketAmbiguous, stage: "socket_table_parse", fault: func(environment *listenerEnvironment) {
			environment.readSocketTables = func(int, hostfacts.Network, map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error) {
				return nil, listenerError(ReasonSocketAmbiguous, injectedDiagnostic("socket_table_parse", ""))
			}
		}},
		{name: "socket table stability re-read", reason: ReasonSocketTable, stage: "socket_table_read", fault: func(environment *listenerEnvironment) {
			calls := 0
			base := environment.readSocketTables
			environment.readSocketTables = func(pid int, network hostfacts.Network, ports map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error) {
				calls++
				if calls == 2 {
					return nil, listenerError(ReasonSocketTable, injectedDiagnostic("socket_table_read", "EIO"))
				}
				return base(pid, network, ports)
			}
		}},
		{name: "fd stability recheck", reason: ReasonDescriptorInventory, stage: "descriptor_inventory", fault: func(environment *listenerEnvironment) {
			calls := 0
			base := environment.readDescriptorSnapshot
			environment.readDescriptorSnapshot = func(pid int) (processevidence.DescriptorSnapshot, error) {
				calls++
				if calls == 2 {
					return processevidence.DescriptorSnapshot{}, listenerError(ReasonDescriptorInventory, injectedDiagnostic("descriptor_inventory", "EIO"))
				}
				return base(pid)
			}
		}},
		{name: "process generation after", reason: ReasonProcessChanged, stage: "process_generation_after", retryCount: 2, fault: func(environment *listenerEnvironment) {
			calls := 0
			environment.readProcessStart = func(int) (string, error) {
				calls++
				if calls%2 == 0 {
					return "", unix.ESRCH
				}
				return "100", nil
			}
		}},
		{name: "pidfd liveness recheck", reason: ReasonProcessChanged, stage: "process_generation_after", retryCount: 2, fault: func(environment *listenerEnvironment) {
			environment.pidfdAlive = func(int) bool { return false }
		}},
		{name: "descriptor inode stability", reason: ReasonObservationChanged, stage: "stability_fence", retryCount: 2, fault: func(environment *listenerEnvironment) {
			calls := 0
			environment.readDescriptorSnapshot = func(int) (processevidence.DescriptorSnapshot, error) {
				calls++
				inode := "100"
				if calls%2 == 0 {
					inode = "101"
				}
				return processevidence.DescriptorSnapshot{Count: 1, SocketInodes: map[int]string{3: inode}}, nil
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := stableFaultEnvironment()
			test.fault(&environment)
			_, err := observeProcessSocketsWith(context.Background(), 42, hostfacts.NetworkTCP, map[uint16]bool{22: true}, environment)
			reason, diagnostic, ok := DiagnosticOf(err)
			if !ok || reason != test.reason || diagnostic.Stage != test.stage || diagnostic.RetryCount != test.retryCount ||
				diagnostic.ProofMethod != hostfacts.ListenerProofProcFSV1 {
				t.Fatalf("owner diagnostic = reason=%q diagnostic=%#v err=%v", reason, diagnostic, err)
			}
			var typed *Error
			if !errors.As(err, &typed) {
				t.Fatalf("typed owner error was lost: %v", err)
			}
			owner, brokerReason, stage, _, _ := typed.BrokerDiagnostic()
			if owner != "listener_evidence" || brokerReason != string(test.reason) || stage != test.stage {
				t.Fatalf("broker projection = %q %q %q", owner, brokerReason, stage)
			}
		})
	}
}

func injectedDiagnostic(stage, errno string) Diagnostic {
	return Diagnostic{ProofMethod: hostfacts.ListenerProofProcFSV1, Stage: stage, ErrnoClass: errno}
}

func stableFaultEnvironment() listenerEnvironment {
	row := hostfacts.ListenerSocketIdentityV1{ProofMethod: hostfacts.ListenerProofProcFSV1,
		Network: hostfacts.NetworkTCP, Family: hostfacts.FamilyIPv4, Bind: "127.0.0.1", Port: 22,
		Inode: "100", CoverageFamilies: []hostfacts.Family{hostfacts.FamilyIPv4}}
	return listenerEnvironment{
		pidfdOpen: func(int, int) (int, error) { return 7, nil }, closeFD: func(int) error { return nil },
		readProcessStart: func(int) (string, error) { return "100", nil },
		readDescriptorSnapshot: func(int) (processevidence.DescriptorSnapshot, error) {
			return processevidence.DescriptorSnapshot{Count: 1, SocketInodes: map[int]string{3: "100"}}, nil
		},
		readSocketTables: func(int, hostfacts.Network, map[uint16]bool) ([]hostfacts.ListenerSocketIdentityV1, error) {
			return []hostfacts.ListenerSocketIdentityV1{row}, nil
		},
		pidfdGetfd: func(int, int, int) (int, error) { return -1, unix.EPERM },
		inspectDuplicatedSocket: func(int, hostfacts.Network, map[uint16]bool) (hostfacts.ListenerSocketIdentityV1, bool) {
			return hostfacts.ListenerSocketIdentityV1{}, false
		},
		pidfdAlive: func(int) bool { return true },
	}
}

func TestObserveAcceptingTCPReturnsMultipleExactListenersAndExcludesUnrelatedPort(t *testing.T) {
	first, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	unrelated, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer unrelated.Close()

	ports := map[uint16]bool{
		uint16(first.Addr().(*net.TCPAddr).Port):  true,
		uint16(second.Addr().(*net.TCPAddr).Port): true,
	}
	observation, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), ports)
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Sockets) != 2 {
		t.Fatalf("expected two selected listeners, got %#v", observation)
	}
	seen := map[uint16]bool{}
	for _, socket := range observation.Sockets {
		if !ports[socket.Port] || seen[socket.Port] || !hostfacts.ValidListenerSocketIdentity(socket) {
			t.Fatalf("non-exact or duplicate listener result: %#v", observation)
		}
		seen[socket.Port] = true
	}
}

func TestObserveAcceptingTCPBindsAddressQualifiedIPv6(t *testing.T) {
	listener, err := net.ListenTCP("tcp6", &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)

	observation, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Sockets) != 1 {
		t.Fatalf("expected one address-qualified IPv6 listener, got %#v", observation)
	}
	socket := observation.Sockets[0]
	if socket.Family != hostfacts.FamilyIPv6 || socket.Bind != "::1" || socket.Wildcard ||
		len(socket.CoverageFamilies) != 1 || socket.CoverageFamilies[0] != hostfacts.FamilyIPv6 ||
		!hostfacts.ValidListenerSocketIdentity(socket) {
		t.Fatalf("IPv6 listener authority is not exact: %#v", observation)
	}
}

func TestObserveAcceptingTCPReportsDualStackWildcardConservatively(t *testing.T) {
	fd, port := dualStackListener(t)
	defer unix.Close(fd)

	observation, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Sockets) != 1 {
		t.Fatalf("expected one dual-stack kernel socket, got %#v", observation)
	}
	socket := observation.Sockets[0]
	if socket.Family != hostfacts.FamilyIPv6 || socket.Bind != "::" || !socket.Wildcard || !hostfacts.ValidListenerSocketIdentity(socket) {
		t.Fatalf("dual-stack wildcard socket lost its exact kernel identity: %#v", observation)
	}
	switch socket.ProofMethod {
	case hostfacts.ListenerProofPIDFDSocketV1:
		if socket.IPv6Only == nil || *socket.IPv6Only || len(socket.CoverageFamilies) != 2 {
			t.Fatalf("descriptor proof did not retain IPV6_V6ONLY=false: %#v", socket)
		}
	case hostfacts.ListenerProofProcFSV1:
		if socket.IPv6Only != nil || len(socket.CoverageFamilies) != 1 || socket.CoverageFamilies[0] != hostfacts.FamilyIPv6 {
			t.Fatalf("procfs proof inferred unavailable dual-stack coverage: %#v", socket)
		}
	default:
		t.Fatalf("unexpected proof method: %#v", socket)
	}
}

func TestObserveAcceptingTCPSeparatesUnrelatedProcessAndRejectsStalePID(t *testing.T) {
	command, port := startListenerContractHelper(t)
	observation, err := ObserveAcceptingTCPDetailed(context.Background(), command.Process.Pid, map[uint16]bool{port: true})
	if err != nil || len(observation.Sockets) != 1 {
		stopListenerContractHelper(t, command)
		t.Fatalf("child listener authority failed: observation=%#v err=%v", observation, err)
	}
	if local, localErr := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true}); localErr != nil || len(local.Sockets) != 0 {
		stopListenerContractHelper(t, command)
		t.Fatalf("unrelated parent acquired child authority: observation=%#v err=%v", local, localErr)
	}
	stopListenerContractHelper(t, command)
	if _, err := ObserveAcceptingTCPDetailed(context.Background(), command.Process.Pid, map[uint16]bool{port: true}); err == nil {
		t.Fatal("exited process retained listener authority")
	} else if reason, _, ok := DiagnosticOf(err); !ok || reason != ReasonPIDFDAuthority && reason != ReasonProcessChanged {
		t.Fatalf("stale process failed without a bounded generation classification: %v", err)
	}
}

func TestObserveAcceptingTCPDetectsClosedAndReplacedSocketIdentity(t *testing.T) {
	first, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(first.Addr().(*net.TCPAddr).Port)
	before, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil || len(before.Sockets) != 1 {
		first.Close()
		t.Fatalf("initial listener authority failed: observation=%#v err=%v", before, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(port)})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	after, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil || len(after.Sockets) != 1 {
		t.Fatalf("replacement listener authority failed: observation=%#v err=%v", after, err)
	}
	if before.Sockets[0].Inode == after.Sockets[0].Inode || before.Sockets[0].Cookie != 0 && before.Sockets[0].Cookie == after.Sockets[0].Cookie {
		t.Fatalf("closed socket authority survived replacement: before=%#v after=%#v", before, after)
	}
}

func TestObserveAcceptingTCPRetainsBothReusePortSocketIdentities(t *testing.T) {
	first, second, port := reusePortListeners(t)
	defer unix.Close(first)
	defer unix.Close(second)

	observation, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Sockets) != 2 || observation.Sockets[0].Inode == observation.Sockets[1].Inode {
		t.Fatalf("same-tuple sockets were collapsed before the semantic owner could reject ambiguity: %#v", observation)
	}
	for _, socket := range observation.Sockets {
		if socket.Family != hostfacts.FamilyIPv4 || socket.Bind != "127.0.0.1" || socket.Port != port || !hostfacts.ValidListenerSocketIdentity(socket) {
			t.Fatalf("reuse-port socket identity is not exact: %#v", observation)
		}
	}
}

func TestObserveAcceptingTCPRejectsClosedListener(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	observation, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Sockets) != 0 {
		t.Fatalf("closed listener retained authority: %#v", observation)
	}
}

func TestObserveAcceptingTCPFallsBackWhenKernelDeniesPidfdGetfd(t *testing.T) {
	const helperEnvironment = "SOLOVEY_LISTENER_SECCOMP_HELPER"
	if os.Getenv(helperEnvironment) != "1" {
		command := exec.Command(os.Args[0], "-test.run", "^TestObserveAcceptingTCPFallsBackWhenKernelDeniesPidfdGetfd$", "-test.v")
		command.Env = append(os.Environ(), helperEnvironment+"=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("seccomp listener helper failed: %v\n%s", err, output)
		}
		return
	}
	installPidfdGetfdDenyFilter(t)
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	observation, err := ObserveAcceptingTCPDetailed(context.Background(), os.Getpid(), map[uint16]bool{port: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Sockets) != 1 || observation.Sockets[0].ProofMethod != hostfacts.ListenerProofProcFSV1 ||
		observation.Sockets[0].Cookie != 0 || !observation.Diagnostic.FallbackUsed || observation.Diagnostic.ErrnoClass != "EPERM" ||
		observation.Diagnostic.DuplicateAttempts == 0 || observation.Diagnostic.DuplicatedSockets != 0 {
		t.Fatalf("kernel-denied getfd did not use exact procfs proof: %#v", observation)
	}
}

func installPidfdGetfdDenyFilter(t *testing.T) {
	t.Helper()
	// no_new_privs is thread-local. Keep installation on that thread, then
	// synchronize the filter so a Go goroutine migration cannot escape it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: unix.SYS_PIDFD_GETFD},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	failedThread, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	if errno != 0 || failedThread != 0 {
		t.Fatalf("install synchronized pidfd_getfd seccomp filter: thread=%d errno=%v", failedThread, errno)
	}
}

func TestListenerContractHelperProcess(t *testing.T) {
	if os.Getenv(listenerContractHelperEnvironment) != "1" {
		return
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		os.Exit(2)
	}
	fmt.Println(listener.Addr().(*net.TCPAddr).Port)
	select {}
}

func startListenerContractHelper(t *testing.T) (*exec.Cmd, uint16) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run", "^TestListenerContractHelperProcess$")
	command.Env = append(os.Environ(), listenerContractHelperEnvironment+"=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(line), 10, 16)
	if err != nil || parsed == 0 {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("listener helper returned an invalid port: %q", line)
	}
	return command, uint16(parsed)
}

func stopListenerContractHelper(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Kill()
	_ = command.Wait()
}

func dualStackListener(t *testing.T) (int, uint16) {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET6, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 0); err != nil {
		unix.Close(fd)
		t.Fatal(err)
	}
	if err := unix.Bind(fd, &unix.SockaddrInet6{Port: 0}); err != nil {
		unix.Close(fd)
		t.Fatal(err)
	}
	if err := unix.Listen(fd, 8); err != nil {
		unix.Close(fd)
		t.Fatal(err)
	}
	address, err := unix.Getsockname(fd)
	if err != nil {
		unix.Close(fd)
		t.Fatal(err)
	}
	return fd, uint16(address.(*unix.SockaddrInet6).Port)
}

func reusePortListeners(t *testing.T) (int, int, uint16) {
	t.Helper()
	create := func(port int) (int, uint16) {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		if err := unix.Bind(fd, &unix.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}}); err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		if err := unix.Listen(fd, 8); err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		address, err := unix.Getsockname(fd)
		if err != nil {
			unix.Close(fd)
			t.Fatal(err)
		}
		return fd, uint16(address.(*unix.SockaddrInet4).Port)
	}
	first, port := create(0)
	second, secondPort := create(int(port))
	if port != secondPort {
		unix.Close(first)
		unix.Close(second)
		t.Fatalf("reuse-port sockets bound different ports: %d != %d", port, secondPort)
	}
	return first, second, port
}
