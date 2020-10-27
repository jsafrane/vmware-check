package check

import (
	"context"
	"fmt"
	"strings"

	"github.com/jsafrane/vmware-check/pkg/clients"
	"github.com/jsafrane/vmware-check/pkg/vmware"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/vsphere"
)

// CheckProviderID tests that Nodes have spec.providerID,
// i.e. they run with a cloud provider.
func CheckNodes(clients clients.Interface, vmClient *govmomi.Client, config *vsphere.VSphereConfig) error {
	klog.V(4).Infof("CheckNodes started")

	nodes, err := clients.ListNodes()
	if err != nil {
		return err
	}
	badNodes := 0
	for i := range nodes {
		node := &nodes[i]

		err := checkNode(node, vmClient, config)
		if err != nil {
			badNodes++
			klog.V(2).Infof("Error on node %q: %s", node.Name, err)
		}
	}

	if badNodes > 0 {
		return fmt.Errorf("%d nodes have issues", badNodes)
	}
	klog.Infof("CheckNodes succeeded, %d nodes checked", len(nodes))
	return nil
}

func checkNode(node *v1.Node, vmClient *govmomi.Client, config *vsphere.VSphereConfig) error {
	klog.V(4).Infof("Checking node %q", node.Name)
	if node.Spec.ProviderID == "" {
		return fmt.Errorf("the node has no providerID")
	}
	klog.V(4).Infof("... the node has providerID: %s", node.Spec.ProviderID)

	if !strings.HasPrefix(node.Spec.ProviderID, "vsphere://") {
		return fmt.Errorf("the node's providerID does not start with vsphere://")
	}

	if err := checkDiskUUID(node, vmClient, config); err != nil {
		return err
	}
	return nil
}

func checkDiskUUID(node *v1.Node, vmClient *govmomi.Client, config *vsphere.VSphereConfig) error {
	vm, err := getVM(node, vmClient, config)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()
	var o mo.VirtualMachine
	err = vm.Properties(ctx, vm.Reference(), []string{"config.extraConfig", "config.flags"}, &o)
	if err != nil {
		return fmt.Errorf("failed to load VM %s: %s", node.Name, err)
	}

	if o.Config.Flags.DiskUuidEnabled == nil {
		return fmt.Errorf("node %q has empty disk.enableUUID", node.Name)
	}
	if *o.Config.Flags.DiskUuidEnabled == false {
		return fmt.Errorf("node %q has disk.enableUUID = FALSE", node.Name)
	}
	klog.V(4).Infof("... the node has correct disk.enableUUID")

	return nil
}

func getVM(node *v1.Node, vmClient *govmomi.Client, config *vsphere.VSphereConfig) (*object.VirtualMachine, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()

	finder := find.NewFinder(vmClient.Client, false)
	dc, err := finder.Datacenter(ctx, config.Workspace.Datacenter)
	if err != nil {
		return nil, fmt.Errorf("failed to access Datacenter %s: %s", config.Workspace.Datacenter, err)
	}
	s := object.NewSearchIndex(dc.Client())
	vmUUID := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(node.Spec.ProviderID, "vsphere://")))
	svm, err := s.FindByUuid(ctx, dc, vmUUID, true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to find VM by UUID %s: %s", vmUUID, err)
	}
	if svm == nil {
		return nil, fmt.Errorf("fnable to find VM by UUID %s", vmUUID)
	}
	return object.NewVirtualMachine(vmClient.Client, svm.Reference()), nil
}
