/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2017 Red Hat, Inc.
 *
 */

package cmdclient

//go:generate mockgen -source $GOFILE -package=$GOPACKAGE -destination=generated_mock_$GOFILE

/*
 ATTENTION: Rerun code generators when interface signatures are modified.
*/

import (
	goerror "errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/rpc"
	"path/filepath"
	"strings"

	k8sv1 "k8s.io/api/core/v1"

	"kubevirt.io/kubevirt/pkg/api/v1"
	diskutils "kubevirt.io/kubevirt/pkg/ephemeral-disk-utils"
	"kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"
)

type Reply struct {
	Success bool
	Message string
	Domain  *api.Domain
}

type Args struct {
	// used for domain management
	VM        *v1.VirtualMachine
	K8Secrets map[string]*k8sv1.Secret

	// used for syncing secrets
	SecretUsageType string
	SecretUsageID   string
	SecretValue     string
}

type LauncherClient interface {
	SyncVirtualMachine(vm *v1.VirtualMachine, secrets map[string]*k8sv1.Secret) error
	ShutdownVirtualMachine(vm *v1.VirtualMachine) error
	KillVirtualMachine(vm *v1.VirtualMachine) error
	SyncSecret(vm *v1.VirtualMachine, usageType string, usageID string, secretValue string) error
	GetDomain() (*api.Domain, bool, error)
	Ping() error
	Close()
}

type VirtLauncherClient struct {
	client *rpc.Client
}

func ListAllSockets(baseDir string) ([]string, error) {
	var socketFiles []string

	fileDir := filepath.Join(baseDir, "sockets")
	exists, err := diskutils.FileExists(fileDir)
	if err != nil {
		return nil, err
	}

	if exists == false {
		return socketFiles, nil
	}

	files, err := ioutil.ReadDir(fileDir)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		socketFiles = append(socketFiles, filepath.Join(fileDir, file.Name()))
	}
	return socketFiles, nil
}

func SocketsDirectory(baseDir string) string {
	return filepath.Join(baseDir, "sockets")
}

func SocketFromNamespaceName(baseDir string, namespace string, name string) string {
	sockFile := namespace + "_" + name + "_sock"
	return filepath.Join(SocketsDirectory(baseDir), sockFile)
}

func DomainFromSocketPath(socketPath string) (*api.Domain, error) {
	splitName := strings.SplitN(filepath.Base(socketPath), "_", 3)
	if len(splitName) != 3 {
		return nil, goerror.New(fmt.Sprintf("malformed domain socket %s", socketPath))
	}
	namespace := splitName[0]
	name := splitName[1]
	domain := api.NewDomainReferenceFromName(namespace, name)

	return domain, nil
}

func GetClient(socketPath string) (LauncherClient, error) {
	conn, err := rpc.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}

	return &VirtLauncherClient{client: conn}, nil
}

func (c *VirtLauncherClient) Close() {
	c.client.Close()
}

func (c *VirtLauncherClient) genericSendCmd(args *Args, cmd string) (*Reply, error) {
	reply := &Reply{}

	err := c.client.Call(cmd, args, reply)
	if IsDisconnected(err) {
		return reply, err
	} else if err != nil {
		msg := fmt.Sprintf("unknown error encountered sending command %s: %s", cmd, err.Error())
		return reply, fmt.Errorf(msg)
	} else if reply.Success != true {
		msg := fmt.Sprintf("server error. command %s failed: %s", cmd, reply.Message)
		return reply, fmt.Errorf(msg)
	}
	return reply, nil
}

func (c *VirtLauncherClient) ShutdownVirtualMachine(vm *v1.VirtualMachine) error {
	cmd := "Launcher.Shutdown"

	args := &Args{
		VM: vm,
	}
	_, err := c.genericSendCmd(args, cmd)

	return err
}

func (c *VirtLauncherClient) KillVirtualMachine(vm *v1.VirtualMachine) error {
	cmd := "Launcher.Kill"

	args := &Args{
		VM: vm,
	}
	_, err := c.genericSendCmd(args, cmd)

	return err
}

func (c *VirtLauncherClient) GetDomain() (*api.Domain, bool, error) {
	domain := &api.Domain{}
	cmd := "Launcher.GetDomain"
	exists := false

	args := &Args{}

	reply, err := c.genericSendCmd(args, cmd)
	if err != nil {
		return nil, exists, err
	}

	if reply.Domain != nil {
		domain = reply.Domain
		exists = true
	}
	return domain, exists, nil

}
func (c *VirtLauncherClient) SyncVirtualMachine(vm *v1.VirtualMachine, secrets map[string]*k8sv1.Secret) error {

	cmd := "Launcher.Sync"

	args := &Args{
		VM:        vm,
		K8Secrets: secrets,
	}

	_, err := c.genericSendCmd(args, cmd)

	return err
}

func (c *VirtLauncherClient) SyncSecret(vm *v1.VirtualMachine, usageType string, usageID string, secretValue string) error {
	cmd := "Launcher.SyncSecret"

	args := &Args{
		VM:              vm,
		SecretUsageType: usageType,
		SecretUsageID:   usageID,
		SecretValue:     secretValue,
	}

	_, err := c.genericSendCmd(args, cmd)
	return err
}

func IsDisconnected(err error) bool {
	if err == rpc.ErrShutdown || err == io.ErrUnexpectedEOF || err == io.EOF {
		return true
	}
	return false
}

func (c *VirtLauncherClient) Ping() error {
	cmd := "Launcher.Ping"
	args := &Args{}
	_, err := c.genericSendCmd(args, cmd)

	return err
}