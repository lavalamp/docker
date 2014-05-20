// +build linux

package nsinit

import (
	"fmt"
	"os"
	"syscall"

	"github.com/dotcloud/docker/pkg/label"
	"github.com/dotcloud/docker/pkg/libcontainer"
	"github.com/dotcloud/docker/pkg/libcontainer/mount"
	"github.com/dotcloud/docker/pkg/system"
)

// ExecIn uses an existing pid and joins the pid's namespaces with the new command.
func ExecIn(container *libcontainer.Container, nspid int, args []string) (int, error) {
	// clear the current processes env and replace it with the environment
	// defined on the container
	if err := LoadContainerEnvironment(container); err != nil {
		return -1, err
	}

	for key, enabled := range container.Namespaces {
		// skip the PID namespace on unshare because it it not supported
		if enabled && key != "NEWPID" {
			if ns := libcontainer.GetNamespace(key); ns != nil {
				if err := system.Unshare(ns.Value); err != nil {
					return -1, err
				}
			}
		}
	}
	processLabel, err := label.GetPidCon(nspid)
	if err != nil {
		return -1, err
	}

	// if the container has a new pid and mount namespace we need to
	// remount proc and sys to pick up the changes
	if container.Namespaces["NEWNS"] && container.Namespaces["NEWPID"] {
		pid, err := system.Fork()
		if err != nil {
			return -1, err
		}
		if pid == 0 {
			// TODO: make all raw syscalls to be fork safe
			if err := system.Unshare(syscall.CLONE_NEWNS); err != nil {
				return -1, err
			}
			if err := mount.RemountProc(); err != nil {
				return -1, fmt.Errorf("remount proc %s", err)
			}
			if err := mount.RemountSys(); err != nil {
				return -1, fmt.Errorf("remount sys %s", err)
			}
			goto dropAndExec
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			return -1, err
		}
		state, err := proc.Wait()
		if err != nil {
			return -1, err
		}
		os.Exit(state.Sys().(syscall.WaitStatus).ExitStatus())
	}

dropAndExec:
	err = label.SetProcessLabel(processLabel)
	if err != nil {
		return -1, err
	}

	nsenter_args := append([]string{"nsenter", "--target", fmt.Sprintf("%v", nspid), "--mount", "--uts", "--ipc", "--net", "--pid"}, args...)
	if err := system.Execv(nsenter_args[0], nsenter_args[0:], container.Env); err != nil {
		return -1, err
	}
	panic("unreachable")
}
