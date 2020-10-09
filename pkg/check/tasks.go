package check

import (
	"context"
	"fmt"

	"github.com/jsafrane/vmware-check/pkg/vmware"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/types"
	"k8s.io/klog/v2"
)

func CheckTaskPermissions(vmClient *govmomi.Client) error {
	klog.V(4).Infof("CheckTaskPermissions started")

	ctx, cancel := context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()

	mgr := view.NewManager(vmClient.Client)
	view, err := mgr.CreateTaskView(ctx, vmClient.ServiceContent.TaskManager)
	if err != nil {
		return fmt.Errorf("error creating task view: %s", err)
	}

	taskCount := 0
	ctx, cancel = context.WithTimeout(context.Background(), *vmware.Timeout)
	defer cancel()
	err = view.Collect(ctx, func(tasks []types.TaskInfo) {
		for _, task := range tasks {
			klog.V(4).Infof("Found task %s", task.Name)
			taskCount++
		}
	})

	if err != nil {
		return fmt.Errorf("error collecting tasks: %s", err)
	}
	klog.Infof("CheckTaskPermissions succeeded, %d tasks found", taskCount)
	return nil
}
