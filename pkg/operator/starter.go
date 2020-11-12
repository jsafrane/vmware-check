package operator

import (
	"context"
	"fmt"
	"time"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformer "github.com/openshift/client-go/config/informers/externalversions"
	operatorclient "github.com/openshift/client-go/operator/clientset/versioned"
	informer "github.com/openshift/client-go/operator/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/loglevel"
	"github.com/openshift/library-go/pkg/operator/management"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/client-go/kubernetes"

	"k8s.io/klog/v2"
)

const (
	resync               = 1 * time.Minute
	operatorName         = "vSphereMonitoringOperator"
	operatorNamespace    = "openshift-cluster-storage-operator"
	cloudConfigNamespace = "openshift-config"
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(controllerConfig.ProtoKubeConfig)
	if err != nil {
		return err
	}
	kubeInformers := v1helpers.NewKubeInformersForNamespaces(kubeClient, operatorNamespace, cloudConfigNamespace)

	csiConfigClient, err := operatorclient.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}
	csiConfigInformers := informer.NewSharedInformerFactoryWithOptions(csiConfigClient, resync)

	operatorClient := &OperatorClient{
		csiConfigInformers,
		csiConfigClient.OperatorV1(),
	}

	configClient, err := configclient.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}
	configInformers := configinformer.NewSharedInformerFactoryWithOptions(configClient, resync)

	operator := NewVSphereMonitoringController(
		operatorClient,
		kubeClient,
		kubeInformers,
		configInformers.Config().V1().Infrastructures(),
		controllerConfig.EventRecorder,
	)

	logLevelController := loglevel.NewClusterOperatorLoggingController(operatorClient, controllerConfig.EventRecorder)
	// TODO remove this controller once we support Removed
	managementStateController := management.NewOperatorManagementStateController(operatorName, operatorClient, controllerConfig.EventRecorder)
	management.SetOperatorNotRemovable()

	klog.Info("Starting the Informers.")
	for _, informer := range []interface {
		Start(stopCh <-chan struct{})
	}{
		csiConfigInformers,
		kubeInformers,
		configInformers,
	} {
		informer.Start(ctx.Done())
	}

	klog.Info("Starting the controllers")
	for _, controller := range []interface {
		Run(ctx context.Context, workers int)
	}{
		logLevelController,
		managementStateController,
		operator,
	} {
		go controller.Run(ctx, 1)
	}
	<-ctx.Done()

	return fmt.Errorf("stopped")
}
