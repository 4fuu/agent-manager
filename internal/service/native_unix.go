//go:build !windows

package service

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

func currentAccount() (string, error) { return strconv.Itoa(os.Getuid()), nil }

func lockControl(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
