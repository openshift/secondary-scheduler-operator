package configobservercontroller

import (
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	libgoapiserver "github.com/openshift/library-go/pkg/operator/configobserver/apiserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/openshift/secondary-scheduler-operator/pkg/operator/configobservation"
)

type ConfigObserver struct {
	factory.Controller
}

func NewConfigObserver(
	operatorClient v1helpers.OperatorClient,
	configInformer configinformers.SharedInformerFactory,
	resourceSyncer resourcesynccontroller.ResourceSyncer,
	eventRecorder events.Recorder,
) *ConfigObserver {
	preRunCacheSynced := []cache.InformerSynced{
		configInformer.Config().V1().APIServers().Informer().HasSynced,
	}

	c := &ConfigObserver{
		Controller: configobserver.NewConfigObserver(
			"secondary-scheduler",
			operatorClient,
			eventRecorder,
			configobservation.Listers{
				APIServerLister_: configInformer.Config().V1().APIServers().Lister(),
				ResourceSync:     resourceSyncer,
				PreRunCachesSynced: append(preRunCacheSynced,
					operatorClient.Informer().HasSynced,
				),
			},
			[]factory.Informer{
				operatorClient.Informer(),
				configInformer.Config().V1().APIServers().Informer(),
			},
			libgoapiserver.ObserveTLSSecurityProfile,
		),
	}

	return c
}
