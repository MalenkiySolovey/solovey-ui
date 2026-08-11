//go:build windows

package operations

import "golang.org/x/sys/windows"

type systemPIDProbe struct{}

const stillActiveExitCode = 259

func (systemPIDProbe) Alive(pid int) (bool, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER {
			return false, nil
		}
		return false, err
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false, err
	}
	return exitCode == stillActiveExitCode, nil
}
