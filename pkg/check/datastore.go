package check

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jsafrane/vmware-check/pkg/clients"
	"github.com/jsafrane/vmware-check/pkg/vmware"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/pbm"
	"github.com/vmware/govmomi/pbm/types"
	"github.com/vmware/govmomi/view"
	vim "github.com/vmware/govmomi/vim25/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/vsphere"
)

const (
	dsParameter            = "datastore"
	storagePolicyParameter = "storagepolicyname"
)

// CheckStorageClasses tests that datastore name in storage classes is short enough.
func CheckStorageClasses(clients clients.Interface, vmClient *govmomi.Client, config *vsphere.VSphereConfig) error {
	var errs []error
	klog.V(4).Infof("CheckStorageClasses started")

	infra, err := clients.GetInfrastructure()
	if err != nil {
		return err
	}

	scs, err := clients.ListStorageClasses()
	if err != nil {
		return err
	}
	for i := range scs {
		sc := &scs[i]
		if sc.Provisioner != "kubernetes.io/vsphere-volume" {
			klog.V(4).Infof("Skipping storage class %q: not a vSphere class", sc.Name)
			continue
		}

		for k, v := range sc.Parameters {
			switch strings.ToLower(k) {
			case dsParameter:
				if err := checkDataStore(v, infra); err != nil {
					errs = append(errs, fmt.Errorf("StorageClass %q is invalid: %s", sc.Name, err))
				}
			case storagePolicyParameter:
				if err := checkStoragePolicy(v, infra, vmClient); err != nil {
					errs = append(errs, fmt.Errorf("StorageClass %q is invalid: %s", sc.Name, err))
				}
			}
		}
	}
	if len(errs) != 0 {
		return errors.NewAggregate(errs)
	}
	klog.V(4).Infof("CheckStorageClasses succeeded, %d storage classes checked", len(scs))
	return nil
}

// CheckPVs tests that datastore name in existing PVs is short enough.
func CheckPVs(clients clients.Interface, vmClient *govmomi.Client, config *vsphere.VSphereConfig) error {
	var errs []error
	klog.V(4).Infof("CheckPVs started")

	pvs, err := clients.ListPVs()
	if err != nil {
		return err
	}
	for i := range pvs {
		pv := &pvs[i]
		if pv.Spec.VsphereVolume == nil {
			continue
		}
		klog.V(4).Infof("Checking PV %q : %s", pv.Name, pv.Spec.VsphereVolume.VolumePath)
		err := checkVolumeName(pv.Spec.VsphereVolume.VolumePath)
		if err != nil {
			errs = append(errs, fmt.Errorf("error checkin PV %q: %s", pv.Name, err))
		}
	}
	if len(errs) != 0 {
		return errors.NewAggregate(errs)
	}
	klog.V(4).Infof("CheckPVs succeeded, %d PVs checked", len(pvs))
	return nil
}

// CheckDefaultDatastore checks that the default data store name is short enough.
func CheckDefaultDatastore(clients clients.Interface, config *vsphere.VSphereConfig) error {
	klog.V(4).Infof("CheckDefaultDatastore started")
	infra, err := clients.GetInfrastructure()
	if err != nil {
		return err
	}

	dsName := config.Workspace.DefaultDatastore
	if err := checkDataStore(dsName, infra); err != nil {
		return fmt.Errorf("Default data store %q is invalid: %s", dsName, err)
	}
	klog.V(4).Infof("CheckDefaultDatastore succeeded")
	return nil
}

// checkStoragePolicy lists all compatible datastores and checks their names are short.
func checkStoragePolicy(policyName string, infrastructure *configv1.Infrastructure, vmClient *govmomi.Client) error {
	klog.V(4).Infof("Checking storage policy %q", policyName)

	pbm, err := getPolicy(policyName, vmClient)
	if err != nil {
		return err
	}
	if len(pbm) == 0 {
		return fmt.Errorf("error listing storage policy %q: policy not found", policyName)
	}
	if len(pbm) > 1 {
		return fmt.Errorf("error listing storage policy %q: multiple (%d) policies found", policyName, len(pbm))
	}

	dataStores, err := getPolicyDatastores(pbm[0].GetPbmProfile().ProfileId, vmClient)
	if err != nil {
		return err
	}
	klog.V(4).Infof("Policy %q is compatible with datastores %v", policyName, dataStores)

	var errs []error
	for _, dataStore := range dataStores {
		err := checkDataStore(dataStore, infrastructure)
		if err != nil {
			errs = append(errs, fmt.Errorf("storage policy %q: %s", policyName, err))
		}
	}
	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}
	return nil
}

// checkStoragePolicy lists all datastores compatible with given policy.
func getPolicyDatastores(profileID types.PbmProfileId, vmClient *govmomi.Client) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()

	c, err := pbm.NewClient(ctx, vmClient.Client)
	if err != nil {
		return nil, err
	}

	// Load all datastores in vSphere
	kind := []string{"Datastore"}
	m := view.NewManager(vmClient.Client)

	ctx, cancel = context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()
	v, err := m.CreateContainerView(ctx, vmClient.Client.ServiceContent.RootFolder, kind, true)
	if err != nil {
		return nil, err
	}

	var content []vim.ObjectContent
	ctx, cancel = context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()
	err = v.Retrieve(ctx, kind, []string{"name"}, &content)
	_ = v.Destroy(ctx)
	if err != nil {
		return nil, err
	}

	// Store the datastores in this map HubID -> DatastoreName
	datastoreNames := make(map[string]string)
	var hubs []types.PbmPlacementHub

	for _, ds := range content {
		hubs = append(hubs, types.PbmPlacementHub{
			HubType: ds.Obj.Type,
			HubId:   ds.Obj.Value,
		})
		datastoreNames[ds.Obj.Value] = ds.PropSet[0].Val.(string)
	}

	req := []types.BasePbmPlacementRequirement{
		&types.PbmPlacementCapabilityProfileRequirement{
			ProfileId: profileID,
		},
	}

	ctx, cancel = context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()
	res, err := c.CheckRequirements(ctx, hubs, nil, req)
	if err != nil {
		return nil, err
	}

	var dataStores []string
	for _, hub := range res.CompatibleDatastores() {
		datastoreName := datastoreNames[hub.HubId]
		dataStores = append(dataStores, datastoreName)
	}
	return dataStores, nil
}

func getPolicy(name string, vmClient *govmomi.Client) ([]types.BasePbmProfile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()

	c, err := pbm.NewClient(ctx, vmClient.Client)
	if err != nil {
		return nil, err
	}
	rtype := types.PbmProfileResourceType{
		ResourceType: string(types.PbmProfileResourceTypeEnumSTORAGE),
	}
	category := types.PbmProfileCategoryEnumREQUIREMENT

	ids, err := c.QueryProfile(ctx, rtype, string(category))
	if err != nil {
		return nil, err
	}

	profiles, err := c.RetrieveContent(ctx, ids)
	if err != nil {
		return nil, err
	}

	for _, p := range profiles {
		if p.GetPbmProfile().Name == name {
			return []types.BasePbmProfile{p}, nil
		}
	}
	return c.RetrieveContent(ctx, []types.PbmProfileId{{UniqueId: name}})
}

var (
	cache sets.String = sets.NewString()
)

func checkDataStore(dsName string, infrastructure *configv1.Infrastructure) error {
	klog.V(4).Infof("Checking datastore %q", dsName)
	if cache.Has(dsName) {
		klog.V(4).Infof("Skipping check of already checked datastore %q", dsName)
		return nil
	}
	cache.Insert(dsName)

	clusterID := infrastructure.Status.InfrastructureName
	volumeName := fmt.Sprintf("[%s] 5137595f-7ce3-e95a-5c03-06d835dea807/%s-dynamic-pvc-8533f1d0-178d-460b-8403-bc5e7dc7f778.vmdk", dsName, clusterID)
	klog.V(4).Infof("Checking data store %q with potential volume name %s", dsName, volumeName)
	if err := checkVolumeName(volumeName); err != nil {
		return fmt.Errorf("error checking datastore %q: %s", dsName, err)
	}
	return nil
}

func checkVolumeName(name string) error {
	path := fmt.Sprintf("/var/lib/kubelet/plugins/kubernetes.io/vsphere-volume/mounts/%s", name)
	escapedPath, err := systemdEscape(path)
	if err != nil {
		return fmt.Errorf("error running systemd-escape: %s", err)
	}
	if len(path) >= 255 {
		return fmt.Errorf("escaped volume path %q is too long (must be under 255 characters, got %d)", escapedPath, len(escapedPath))
	}
	return nil

}

func systemdEscape(path string) (string, error) {
	cmd := exec.Command("systemd-escape", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("systemd-escape: %s: %s", err, string(out))
	}
	escapedPath := strings.TrimSpace(string(out))
	klog.V(4).Infof("path %q systemd-escaped to %q (%d)", path, escapedPath, len(escapedPath))
	return escapedPath, nil
}
