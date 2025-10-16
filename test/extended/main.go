package extended

import (
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

var _ = g.Describe("[Jira:secondary-scheduler][sig-secondary-scheduler] sanity test", func() {
	g.It("should always pass [Suite:openshift/secondary-scheduler-operator/conformance/parallel]", func() {
		o.Expect(true).To(o.BeTrue())
	})
})
