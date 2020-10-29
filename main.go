package main

import (
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/jsafrane/vmware-check/pkg/check"
	"github.com/jsafrane/vmware-check/pkg/clients"
	"github.com/jsafrane/vmware-check/pkg/vmware"
	ocpv1 "github.com/openshift/api/config/v1"
	"github.com/vmware/govmomi"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/vsphere"
)

const (
	configNamespace = "openshift-config"
)

var (
	vmwareConfig = flag.String("vmware-config", "", "Path to VMware configuration file, as used in OpenShift / Kubernetes cloud provider. It will be downloaded from OCP cluster if omitted.")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	clients, err := clients.Create()
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes clients: %s", err)
	}

	vmConfig, err := getConfig(clients)
	if err != nil {
		klog.Fatalf("Failed to get VMware config: %s", err)
	}

	vmClient, err := connect(clients, vmConfig)
	if err != nil {
		klog.Fatalf("Failed to connect to vSphere: %s", err)
	}

	if err := check.CheckTaskPermissions(vmClient); err != nil {
		klog.Errorf("Check failed: %s", err)
	}
	if err := check.CheckFolderList(vmClient, vmConfig); err != nil {
		klog.Errorf("Check failed: %s", err)
	}
	if err := check.CheckNodes(clients, vmClient, vmConfig); err != nil {
		klog.Errorf("Check failed: %s", err)
	}
	if err := check.CheckDefaultDatastore(clients, vmConfig); err != nil {
		klog.Errorf("Check failed: %s", err)
	}
	if err := check.CheckStorageClasses(clients, vmClient, vmConfig); err != nil {
		klog.Errorf("Check failed: %s", err)
	}
	if err := check.CheckPVs(clients, vmClient, vmConfig); err != nil {
		klog.Errorf("Check failed: %s", err)
	}
}

func connect(clients clients.Interface, cfg *vsphere.VSphereConfig) (*govmomi.Client, error) {
	secret, err := clients.GetSecret(cfg.Global.SecretNamespace, cfg.Global.SecretName)
	if err != nil {
		return nil, fmt.Errorf("Failed to get cluster secret %s/%s: %s", cfg.Global.SecretNamespace, cfg.Global.SecretName, err)
	}
	klog.V(4).Infof("Got Secret %s/%s", cfg.Global.SecretNamespace, cfg.Global.SecretName)

	userKey := cfg.Workspace.VCenterIP + "." + "username"
	username := string(secret.Data[userKey])
	passwordKey := cfg.Workspace.VCenterIP + "." + "password"
	password := string(secret.Data[passwordKey])
	vmClient, err := vmware.NewClient(cfg, username, password)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to %s: %s", cfg.Workspace.VCenterIP, err)
	}
	klog.V(2).Infof("Connected to %s as %s", cfg.Workspace.VCenterIP, username)
	return vmClient, nil
}

func getConfig(clients clients.Interface) (*vsphere.VSphereConfig, error) {
	var cfgData string
	if *vmwareConfig == "" {
		var err error
		klog.V(4).Infof("Trying to get VMware config from the cluster")
		cfg, err := getConfigDataFromCluster(clients)
		if err != nil {
			return nil, err
		}
		cfgData = cfg
	} else {
		var err error
		klog.V(4).Infof("Loading VMware config from %s", *vmwareConfig)
		cfg, err := ioutil.ReadFile(*vmwareConfig)
		if err != nil {
			return nil, err
		}
		cfgData = string(cfg)
	}

	cfg, err := vmware.ParseConfig(cfgData)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse config: %s", err)
	}
	return cfg, nil
}

func getConfigDataFromCluster(clients clients.Interface) (string, error) {
	infra, err := clients.GetInfrastructure()
	if err != nil {
		klog.Fatalf("Failed to get Infrastructure: %s", err)
	}
	klog.V(4).Infof("Got Infrastructure with Platform %q", infra.Status.PlatformStatus.Type)

	if infra.Status.PlatformStatus.Type != ocpv1.VSpherePlatformType {
		klog.Fatalf("Unsupported platform: %s", infra.Status.PlatformStatus.Type)
	}

	configMap, err := clients.GetConfigMap(configNamespace, infra.Spec.CloudConfig.Name)
	if err != nil {
		return "", fmt.Errorf("Failed to get cluster config: %s", err)
	}
	cfgString, found := configMap.Data[infra.Spec.CloudConfig.Key]
	if !found {
		return "", fmt.Errorf("Cluster config %s/%s does not contain key %s", configNamespace, infra.Spec.CloudConfig.Name, infra.Spec.CloudConfig.Key)
	}
	klog.V(4).Infof("Got ConfigMap %s/%s with config:\n%s", configNamespace, infra.Spec.CloudConfig.Name, cfgString)
	return cfgString, nil
}
