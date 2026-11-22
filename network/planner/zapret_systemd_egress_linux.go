//go:build linux && !minimal

package planner

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// CheckZapretEgressAdmission rejects competing tc/TCX owners on the
// approved device. It is a read-only startup check. The trusted administrator
// must retire the installed path before adding privileged egress hooks.
func CheckZapretEgressAdmission(ctx context.Context, ifindex int) error {
	if ifindex <= 0 {
		return errors.New("invalid Zapret egress interface index")
	}
	conn, err := netlink.Dial(unix.NETLINK_ROUTE, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	const tcMessageLen = 20 // struct tcmsg, linux/rtnetlink.h
	request := make([]byte, tcMessageLen)
	binary.NativeEndian.PutUint32(request[4:], uint32(ifindex))
	replies, err := conn.Execute(netlink.Message{Header: netlink.Header{
		Type: unix.RTM_GETQDISC, Flags: netlink.Request | netlink.Dump,
	}, Data: request})
	if err != nil {
		return fmt.Errorf("inventory Zapret egress qdiscs: %w", err)
	}
	for _, reply := range replies {
		if reply.Header.Type != unix.RTM_NEWQDISC || reply.Header.Flags&unix.NLM_F_DUMP_INTR != 0 || len(reply.Data) < tcMessageLen {
			return errors.New("incomplete Zapret qdisc inventory")
		}
		if binary.NativeEndian.Uint32(reply.Data[4:]) != uint32(ifindex) {
			continue
		}
		decoder, err := netlink.NewAttributeDecoder(reply.Data[tcMessageLen:])
		if err != nil {
			return err
		}
		var kind string
		for decoder.Next() {
			if decoder.Type() == unix.TCA_KIND {
				kind = decoder.String()
			}
		}
		if err := decoder.Err(); err != nil {
			return err
		}
		switch kind {
		case "noqueue", "mq", "fq", "fq_codel", "pfifo_fast":
		default:
			return errors.New("a foreign tc queue or classifier conflicts with the Zapret egress profile")
		}
	}
	for _, direction := range []uint32{unix.BPF_TCX_INGRESS, unix.BPF_TCX_EGRESS} {
		// linux/bpf.h BPF_PROG_QUERY. A zero count and nil result buffers
		// request only the attached program count with existing NET_ADMIN.
		query := struct {
			IfIndex, AttachType, QueryFlags, AttachFlags uint32
			ProgramIDs                                   uint64
			Count, Padding                               uint32
			ProgramFlags, LinkIDs, LinkFlags, Revision   uint64
		}{IfIndex: uint32(ifindex), AttachType: direction}
		_, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_PROG_QUERY, uintptr(unsafe.Pointer(&query)), unsafe.Sizeof(query)) // #nosec G103 -- fixed kernel query structure.
		if errno != 0 {
			return fmt.Errorf("cannot establish that the Zapret interface has no TCX owner: %w", errno)
		}
		if query.Count != 0 {
			return errors.New("a foreign TCX program conflicts with the Zapret egress profile")
		}
	}
	return ctx.Err()
}
