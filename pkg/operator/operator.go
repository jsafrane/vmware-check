package operator

import (
	"context"
	"fmt"

	"github.com/jsafrane/vmware-check/pkg/vmware"
	ocpv1 "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	infrainformer "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	infralister "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/vmware/govmomi"
	"k8s.io/client-go/kubernetes"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

type vSphereMonitoringController struct {
	operatorClient       *OperatorClient
	kubeClient           kubernetes.Interface
	infraLister          infralister.InfrastructureLister
	secretLister         corelister.SecretLister
	cloudConfigMapLister corelister.ConfigMapLister
	eventRecorder        events.Recorder
}

const (
	infrastructureName         = "cluster"
	cloudCredentialsSecretName = "vsphere-cloud-credentials"
)

func NewVSphereMonitoringController(
	operatorClient *OperatorClient,
	kubeClient kubernetes.Interface,
	namespacedInformer v1helpers.KubeInformersForNamespaces,
	configInformer infrainformer.InfrastructureInformer,
	eventRecorder events.Recorder) factory.Controller {

	secretInformer := namespacedInformer.InformersFor(operatorNamespace).Core().V1().Secrets()
	cloudConfigMapInformer := namespacedInformer.InformersFor(cloudConfigNamespace).Core().V1().ConfigMaps()
	c := &vSphereMonitoringController{
		operatorClient:       operatorClient,
		kubeClient:           kubeClient,
		secretLister:         secretInformer.Lister(),
		cloudConfigMapLister: cloudConfigMapInformer.Lister(),
		infraLister:          configInformer.Lister(),
		eventRecorder:        eventRecorder.WithComponentSuffix("vSphereMonitoringController"),
	}
	return factory.New().WithSync(c.sync).WithSyncDegradedOnError(operatorClient).WithInformers(
		configInformer.Informer(),
		secretInformer.Informer(),
		cloudConfigMapInformer.Informer(),
	).ToController("vSphereMonitoringController", eventRecorder)
}

func (c *vSphereMonitoringController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	klog.V(4).Infof("vSphereMonitoringController.Sync started")
	defer klog.V(4).Infof("vSphereMonitoringController.Sync finished")

	opSpec, _, _, err := c.operatorClient.GetOperatorState()
	if err != nil {
		return err
	}
	if opSpec.ManagementState != operatorapi.Managed {
		return nil
	}

	_, err = c.connect(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (c *vSphereMonitoringController) connect(ctx context.Context) (*govmomi.Client, error) {
	cfgString, err := c.getVSphereConfig(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := vmware.ParseConfig(cfgString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %s", err)
	}

	secret, err := c.secretLister.Secrets(operatorNamespace).Get(cloudCredentialsSecretName)
	if err != nil {
		return nil, err
	}
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

func (c *vSphereMonitoringController) getVSphereConfig(ctx context.Context) (string, error) {
	infra, err := c.infraLister.Get(infrastructureName)
	if err != nil {
		return "", err
	}
	if infra.Status.PlatformStatus.Type != ocpv1.VSpherePlatformType {
		return "", fmt.Errorf("unsupported platform: %s", infra.Status.PlatformStatus.Type)
	}

	cloudConfigMap, err := c.cloudConfigMapLister.ConfigMaps(cloudConfigNamespace).Get(infra.Spec.CloudConfig.Name)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster config: %s", err)
	}

	cfgString, found := cloudConfigMap.Data[infra.Spec.CloudConfig.Key]
	if !found {
		return "", fmt.Errorf("cluster config %s/%s does not contain key %s", cloudConfigNamespace, infra.Spec.CloudConfig.Name, infra.Spec.CloudConfig.Key)
	}
	klog.V(4).Infof("Got ConfigMap %s/%s with config:\n%s", cloudConfigNamespace, infra.Spec.CloudConfig.Name, cfgString)

	return cfgString, nil
}
