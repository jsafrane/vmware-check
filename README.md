# VMware check

## Usage:
```
$ go get github.com/jsafrane/vmware-check
$ export KUBECONFIG=<my OCP kubeconfig>
$ vmware-check

I1009 12:44:46.796129  389720 tasks.go:39] CheckTaskPermissions succeeded, 55 tasks found
I1009 12:44:46.941914  389720 folder.go:47] Listing Datastore "WorkloadDatastore" succeeded
```

* Use `-v 2` / `-v 4` for more detailed logs.
