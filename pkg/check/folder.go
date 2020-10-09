package check

import (
	"context"
	"fmt"

	"github.com/jsafrane/vmware-check/pkg/vmware"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/vsphere"
)

func CheckFolderList(vmClient *govmomi.Client, config *vsphere.VSphereConfig) error {
	klog.V(4).Infof("CheckFolderList started")

	ctx, cancel := context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()

	finder := find.NewFinder(vmClient.Client, false)
	dc, err := finder.Datacenter(ctx, config.Workspace.Datacenter)
	if err != nil {
		return fmt.Errorf("failed to access Datacenter %s: %s", config.Workspace.Datacenter, err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()
	finder.SetDatacenter(dc)
	ds, err := finder.Datastore(ctx, config.Workspace.DefaultDatastore)
	if err != nil {
		return fmt.Errorf("failed to access Datastore %s: %s", config.Workspace.DefaultDatastore, err)
	}
	// OCP needs permissions to list files, try "/" that must exists.
	err = listDirectory(config, ds, "/", false)
	if err != nil {
		return err
	}

	// OCP needs permissions to list "/kubelet", tolerate if it does not exist.
	err = listDirectory(config, ds, "/kubevols", true)
	if err != nil {
		return err
	}

	klog.Infof("Listing Datastore %q succeeded", config.Workspace.DefaultDatastore)
	return nil
}

func listDirectory(config *vsphere.VSphereConfig, ds *object.Datastore, path string, tolerateNotFound bool) error {
	klog.V(4).Infof("Listing datastore %s path %s", ds.Name(), path)
	ctx, cancel := context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()

	browser, err := ds.Browser(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Datastore %s browser: %s", config.Workspace.DefaultDatastore, err)
	}

	spec := types.HostDatastoreBrowserSearchSpec{
		MatchPattern: []string{"*"},
	}
	ctx, cancel = context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()
	task, err := browser.SearchDatastore(ctx, ds.Path(path), &spec)
	if err != nil {
		if tolerateNotFound && types.IsFileNotFound(err) {
			klog.Infof("Warning: path %s does not exist it Datastore %s", path, config.Workspace.DefaultDatastore)
			return nil
		}
		return fmt.Errorf("failed to browse Datastore %s: %s", config.Workspace.DefaultDatastore, err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()
	info, err := task.WaitForResult(ctx, nil)
	if err != nil {
		if tolerateNotFound && types.IsFileNotFound(err) {
			klog.Infof("Warning: path %s does not exist it Datastore %s", path, config.Workspace.DefaultDatastore)
			return nil
		}
		return fmt.Errorf("failed to list datastore: %s ", err)
	}

	var items []types.HostDatastoreBrowserSearchResults
	switch r := info.Result.(type) {
	case types.HostDatastoreBrowserSearchResults:
		items = []types.HostDatastoreBrowserSearchResults{r}
	case types.ArrayOfHostDatastoreBrowserSearchResults:
		items = r.HostDatastoreBrowserSearchResults
	default:
		return fmt.Errorf("uknown data received from Datastore browser: %T", r)
	}

	for _, i := range items {
		for _, f := range i.File {
			klog.V(4).Infof("Found file %s/%s", path, f.GetFileInfo().Path)
		}
	}
	return nil

}
