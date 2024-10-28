package main

import (
	"context"
	"fmt"
	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/coreos/go-systemd/v22/unit"
	"github.com/digitalocean/go-qemu/qmp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const workingDirectory = "/home/fess932/git/os"
const systemdConfigDirectory = "/home/fess932/.config/systemd/user/"

type vm struct {
	name string
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open src file %q: %v", src, err)
	}
	defer func() {
		if err = in.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close in file")
		}
	}()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to open dst file %q: %v", dst, err)
	}
	defer func() {
		if err = out.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close out file")
		}
	}()
	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("copying %s to %s failed: %v", src, dst, err)
	}

	return nil
}

func newService(vmName string) error {
	// create home directory
	vmWorkingDirectory := filepath.Join(workingDirectory, vmName)

	if err := os.MkdirAll(vmWorkingDirectory, 0777); err != nil {
		return fmt.Errorf("failed to create working directory: %w", err)
	}
	// copy backing file?
	if err := copyFile("/home/fess932/Downloads/noble-server-cloudimg-amd64.img", filepath.Join(vmWorkingDirectory, vmName+".qcow2")); err != nil {
		return fmt.Errorf("failed to copy backing file: %w", err)
	}

	// copy seed.img
	if err := copyFile("/home/fess932/git/os/seed.img", filepath.Join(vmWorkingDirectory, "seed.img")); err != nil {
		return fmt.Errorf("failed to copy seed.img: %w", err)
	}

	// create systemd service
	vmConfig := []string{
		"/usr/bin/qemu-system-x86_64",
		fmt.Sprintf("-qmp unix:%s.socket,server,nowait", vmName),
		"-cpu host -smp 2",
		"-machine type=q35,accel=kvm",
		"-m 2048",
		fmt.Sprintf("-drive if=virtio,format=qcow2,file=%v.qcow2", vmName),
		"-drive if=virtio,format=raw,file=seed.img",
	}

	opts := []*unit.UnitOption{
		{"Unit", "Description", fmt.Sprintf("Qemu agent for vm: %v", vmName)},
		{"Service", "WorkingDirectory", vmWorkingDirectory},
		{"Service", "ExecStart", strings.Join(vmConfig, " ")}, // qemu monitor socket?
		{"Install", "WantedBy", "multi-user.target"},
	}

	servicePath := fmt.Sprintf("%s/%s.service", systemdConfigDirectory, vmName)
	file, err := os.OpenFile(servicePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to create/open file systemd unit: %w", err)
	}
	if _, err = io.Copy(file, unit.Serialize(opts)); err != nil {
		return fmt.Errorf("failed to write systemd unit: %w", err)
	}

	return nil
}

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	log.Print("hello world")
	// create systemd unit for vm   ok
	// reload systemd configuration ok
	// run with systemd with qemu monitor socket ok
	// connect to qemu monitor socket
	// ???

	vmName := "vm3"
	vmNameService := "vm3.service"
	monitorSocketPath := fmt.Sprintf("%s.socket", filepath.Join(workingDirectory, vmName, vmName))

	if err := newService(vmName); err != nil {
		log.Fatal().Err(err).Msg("failed to create systemd unit")
	}

	a, err := dbus.NewUserConnectionContext(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to dbus")
	}

	if err = a.ReloadContext(context.Background()); err != nil {
		log.Fatal().Err(err).Msg("failed to reload dbus")
	}

	_, err = a.StartUnitContext(context.Background(), vmNameService, "replace", nil)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to start hypervisor service")
	}

	log.Printf("connectiong to %s", monitorSocketPath)
	time.Sleep(time.Second * 3) // wait for qemu createing socket monitor
	socketMonitor, err := qmp.NewSocketMonitor("unix", monitorSocketPath, time.Second*3)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to new socket qemu monitor")
	}

	if err = socketMonitor.Connect(); err != nil {
		log.Fatal().Err(err).Msg("failed to connect to qemu monitor")
	}
	defer socketMonitor.Disconnect()

	stream, _ := socketMonitor.Events(context.Background())
	for e := range stream {
		log.Printf("EVENT: %s", e.Event)
	}
}
