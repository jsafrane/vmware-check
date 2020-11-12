package vmware

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vim25/soap"
	"gopkg.in/gcfg.v1"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/vsphere"
)

var (
	Timeout = flag.Duration("vmware-timeout", 10*time.Second, "Timeout of all VMware calls")
)

func ParseConfig(data string) (*vsphere.VSphereConfig, error) {
	var cfg vsphere.VSphereConfig
	err := gcfg.ReadStringInto(&cfg, data)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func NewClient(cfg *vsphere.VSphereConfig, username, password string) (*govmomi.Client, error) {
	serverAddress := cfg.Workspace.VCenterIP
	serverURL, err := soap.ParseURL(serverAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %s", err)
	}
	serverURL.User = url.UserPassword(username, password)

	insecure := cfg.Global.InsecureFlag
	ctx, cancel := context.WithTimeout(context.Background(), *Timeout)
	defer cancel()
	klog.V(4).Infof("Connecting to %s as %s, insecure %t", serverAddress, username, insecure)

	client, err := govmomi.NewClient(ctx, serverURL, insecure)
	if err != nil {
		return nil, err
	}
	return client, nil
}
