module github.com/openshift/secondary-scheduler-operator

go 1.16

require (
	github.com/openshift/api v0.0.0-20210331193751-3acddb19d360
	github.com/openshift/build-machinery-go v0.0.0-20211213093930-7e33a7eb4ce3
	github.com/openshift/client-go v0.0.0-20210331195552-cf6c2669e01f
	github.com/openshift/library-go v0.0.0-20210331235027-66936e2fcc52
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.1.3
	k8s.io/api v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/client-go v0.21.2
	k8s.io/code-generator v0.21.2
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/controller-tools v0.6.1
)
