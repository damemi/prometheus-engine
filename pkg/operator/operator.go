// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package operator

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	arv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	monitoringv1 "github.com/GoogleCloudPlatform/prometheus-engine/pkg/operator/apis/monitoring/v1"
)

const (
	// DefaultOperatorNamespace is the namespace in which all resources owned by the operator are installed.
	DefaultOperatorNamespace = "gmp-system"
	// DefaultPublicNamespace is the namespace where the operator will check for user-specified
	// configuration data.
	DefaultPublicNamespace = "gmp-public"

	// Fixed names used in various resources managed by the operator.
	NameOperator  = "gmp-operator"
	componentName = "managed_prometheus"

	// Prometheus configuration file and volume mounts.
	// Used in both collectors and rule-evaluator.
	configOutDir        = "/prometheus/config_out"
	configVolumeName    = "config"
	configDir           = "/prometheus/config"
	configOutVolumeName = "config-out"
	configFilename      = "config.yaml"
	storageVolumeName   = "storage"
	storageDir          = "/prometheus/data"

	// The well-known app name label.
	LabelAppName = "app.kubernetes.io/name"
	// The component name, will be exposed as metric name.
	AnnotationMetricName = "components.gke.io/component-name"
	// ClusterAutoscalerSafeEvictionLabel is the annotation label that determines
	// whether the cluster autoscaler can safely evict a Pod when the Pod doesn't
	// satisfy certain eviction criteria.
	ClusterAutoscalerSafeEvictionLabel = "cluster-autoscaler.kubernetes.io/safe-to-evict"

	// The official images to be used with this version of the operator. For debugging
	// and emergency use cases they may be overwritten through options.
	ImageCollector      = "gke.gcr.io/prometheus-engine/prometheus:v2.35.0-gmp.2-gke.0"
	ImageConfigReloader = "gke.gcr.io/prometheus-engine/config-reloader:v0.4.2-gke.0"
	ImageRuleEvaluator  = "gke.gcr.io/prometheus-engine/rule-evaluator:v0.4.2-gke.0"

	// The k8s Application, will be exposed as component name.
	KubernetesAppName    = "app"
	CollectorAppName     = "managed-prometheus-collector"
	RuleEvaluatorAppName = "managed-prometheus-rule-evaluator"

	// The Collector version, will be exposed as part of the user agent information.
	CollectorVersion = "2.35.0-gmp.2"
)

// Operator to implement managed collection for Google Prometheus Engine.
type Operator struct {
	logger  logr.Logger
	opts    Options
	client  client.Client
	manager manager.Manager
}

// Options for the Operator.
type Options struct {
	// ID of the project of the cluster.
	ProjectID string
	// Location of the cluster.
	Location string
	// Name of the cluster the operator acts on.
	Cluster string
	// Disable exporting to GCM (mostly for testing).
	DisableExport bool
	// Namespace to which the operator deploys any associated resources.
	OperatorNamespace string
	// Namespace to which the operator looks for user-specified configuration
	// data, like Secrets and ConfigMaps.
	PublicNamespace string
	// Listening port of the collector. Configurable to allow multiple
	// simultanious collector deployments for testing purposes while each
	// collector runs on the host network.
	CollectorPort int32
	// Image for the Prometheus collector container.
	ImageCollector string
	// Image for the Prometheus config reloader.
	ImageConfigReloader string
	// Image for the Prometheus rule-evaluator.
	ImageRuleEvaluator string
	// Whether to deploy pods with hostNetwork enabled. This allow pods to run with the GCE compute
	// default service account even on GKE clusters with Workload Identity enabled.
	// It must be set to false for GKE Autopilot clusters.
	HostNetwork bool
	// Priority class for the collector pods.
	PriorityClass string
	// Certificate of the server in base 64.
	TLSCert string
	// Key of the server in base 64.
	TLSKey string
	// Certificate authority in base 64.
	CACert string
	// Endpoint of the Cloud Monitoring API to be used by all collectors.
	CloudMonitoringEndpoint string
	// Webhook serving address.
	ListenAddr string
	// Collector memory resource
	CollectorMemoryResource int64
	// Collector CPU resource
	CollectorCPUResource int64
	// Collector CPU limit
	CollectorCPULimit int64
	// Collector memory limit
	CollectorMemoryLimit int64
	// Evaluator memory resource
	EvaluatorMemoryResource int64
	// Evaluator CPU resource
	EvaluatorCPUResource int64
	// Evaluator memory limit
	EvaluatorMemoryLimit int64
	// Evaluator CPU limit
	EvaluatorCPULimit int64
	// How managed collection was provisioned.
	Mode string
}

func (o *Options) defaultAndValidate(logger logr.Logger) error {
	if o.OperatorNamespace == "" {
		o.OperatorNamespace = DefaultOperatorNamespace
	}
	if o.PublicNamespace == "" {
		// For non-managed deployments, default to same namespace
		// as operator, assuming cluster operators prefer consolidating
		// resources in a single namespace.
		o.PublicNamespace = DefaultOperatorNamespace
	}
	if o.CollectorPort == 0 {
		o.CollectorPort = 19090
	}
	if o.ImageCollector == "" {
		o.ImageCollector = ImageCollector
	}
	if o.ImageConfigReloader == "" {
		o.ImageConfigReloader = ImageConfigReloader
	}
	if o.ImageRuleEvaluator == "" {
		o.ImageRuleEvaluator = ImageRuleEvaluator
	}

	// ProjectID and Cluster must be always be set. Collectors and rule-evaluator can
	// auto-discover them but we need them in the operator to scope generated rules.
	if o.ProjectID == "" {
		return errors.New("ProjectID must be set")
	}
	if o.Cluster == "" {
		return errors.New("Cluster must be set")
	}

	if o.ImageCollector != ImageCollector {
		logger.Info("not using the canonical collector image",
			"expected", ImageCollector, "got", o.ImageCollector)
	}
	if o.ImageConfigReloader != ImageConfigReloader {
		logger.Info("not using the canonical config reloader image",
			"expected", ImageConfigReloader, "got", o.ImageConfigReloader)
	}
	if o.ImageRuleEvaluator != ImageRuleEvaluator {
		logger.Info("not using the canonical rule-evaluator image",
			"expected", ImageRuleEvaluator, "got", o.ImageRuleEvaluator)
	}
	if o.CollectorCPUResource <= 0 {
		o.CollectorCPUResource = 100
	}
	if o.CollectorMemoryResource <= 0 {
		o.CollectorMemoryResource = 200
	}
	if o.CollectorMemoryLimit <= o.CollectorMemoryResource {
		if o.CollectorMemoryResource*2 < 3000 {
			o.CollectorMemoryLimit = 3000
		} else {
			o.CollectorMemoryLimit = o.CollectorMemoryResource * 2
		}
		o.CollectorMemoryLimit = o.CollectorMemoryResource * 15
	}

	if o.EvaluatorCPUResource <= 0 {
		o.EvaluatorCPUResource = 100
	}
	if o.EvaluatorMemoryResource <= 0 {
		o.EvaluatorMemoryResource = 200
	}
	if o.EvaluatorMemoryLimit <= o.EvaluatorMemoryResource {
		o.EvaluatorMemoryLimit = o.EvaluatorMemoryResource * 15
	}
	switch o.Mode {
	// repo manifest always defaults to "kubectl".
	case "kubectl":
	case "gke":
	case "gke-auto":
	case "on-prem":
	case "baremetal":
	default:
		return errors.New("--mode must be one of {'kubectl', 'gke', 'gke-auto', 'on-prem', 'baremetal}")
	}
	return nil
}

// New instantiates a new Operator.
func New(logger logr.Logger, clientConfig *rest.Config, registry prometheus.Registerer, opts Options) (*Operator, error) {
	if err := opts.defaultAndValidate(logger); err != nil {
		return nil, errors.Wrap(err, "invalid options")
	}
	// Create temporary directory to store webhook serving cert files.
	certDir, err := ioutil.TempDir("", "operator-cert")
	if err != nil {
		return nil, errors.Wrap(err, "create temporary certificate dir")
	}

	sc := runtime.NewScheme()

	if err := scheme.AddToScheme(sc); err != nil {
		return nil, errors.Wrap(err, "add Kubernetes core scheme")
	}
	if err := monitoringv1.AddToScheme(sc); err != nil {
		return nil, errors.Wrap(err, "add monitoringv1 scheme")
	}
	host, portStr, err := net.SplitHostPort(opts.ListenAddr)
	if err != nil {
		return nil, errors.Wrap(err, "invalid listen address")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, errors.Wrap(err, "invalid port")
	}
	mgr, err := ctrl.NewManager(clientConfig, manager.Options{
		Scheme: sc,
		Host:   host,
		Port:   port,
		// Don't run a metrics server with the manager. Metrics are being served
		// explicitly in the main routine.
		MetricsBindAddress: "0",
		CertDir:            certDir,
	})
	if err != nil {
		return nil, errors.Wrap(err, "create controller manager")
	}
	client, err := client.New(clientConfig, client.Options{Scheme: sc})
	if err != nil {
		return nil, errors.Wrap(err, "create client")
	}

	op := &Operator{
		logger:  logger,
		opts:    opts,
		client:  client,
		manager: mgr,
	}
	return op, nil
}

// setupAdmissionWebhooks configures validating webhooks for the operator-managed
// custom resources and registers handlers with the webhook server.
func (o *Operator) setupAdmissionWebhooks(ctx context.Context) error {
	// Delete old ValidatingWebhookConfiguration that was installed directly by the operator
	// in previous versions.
	err := o.client.Delete(ctx, &arv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "gmp-operator"},
	})
	if err != nil && !apierrors.IsNotFound(err) {
		o.logger.Error(err, "msg", "Deleting legacy ValidatingWebhookConfiguration failed")
	}

	// Write provided cert files.
	caBundle, err := o.ensureCerts(ctx, o.manager.GetWebhookServer().CertDir)
	if err != nil {
		return err
	}

	// Keep setting the caBundle in the expected webhook configurations.
	go func() {
		// Only inject if we've an explicit CA bundle ourselves. Otherwise the webhook configs
		// may already have been created with one.
		if len(caBundle) == 0 {
			return
		}
		// Initial sleep for the client to initialize before our first calls.
		// Ideally we could explicitly wait for it.
		time.Sleep(5 * time.Second)

		for {
			if err := o.setValidatingWebhookCABundle(ctx, caBundle); err != nil {
				o.logger.Error(err, "Setting CA bundle for ValidatingWebhookConfiguration failed")
			}
			if err := o.setMutatingWebhookCABundle(ctx, caBundle); err != nil {
				o.logger.Error(err, "Setting CA bundle for MutatingWebhookConfiguration failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
		}
	}()

	s := o.manager.GetWebhookServer()

	// Validating webhooks.
	s.Register(
		validatePath(monitoringv1.PodMonitoringResource()),
		admission.ValidatingWebhookFor(&monitoringv1.PodMonitoring{}),
	)
	s.Register(
		validatePath(monitoringv1.ClusterPodMonitoringResource()),
		admission.ValidatingWebhookFor(&monitoringv1.ClusterPodMonitoring{}),
	)
	s.Register(
		validatePath(monitoringv1.OperatorConfigResource()),
		admission.WithCustomValidator(&monitoringv1.OperatorConfig{}, &operatorConfigValidator{
			namespace: o.opts.PublicNamespace,
		}),
	)
	s.Register(
		validatePath(monitoringv1.RulesResource()),
		admission.WithCustomValidator(&monitoringv1.Rules{}, &rulesValidator{
			opts: o.opts,
		}),
	)
	s.Register(
		validatePath(monitoringv1.ClusterRulesResource()),
		admission.WithCustomValidator(&monitoringv1.ClusterRules{}, &clusterRulesValidator{
			opts: o.opts,
		}),
	)
	s.Register(
		validatePath(monitoringv1.GlobalRulesResource()),
		admission.WithCustomValidator(&monitoringv1.GlobalRules{}, &globalRulesValidator{}),
	)
	// Defaulting webhooks.
	s.Register(
		defaultPath(monitoringv1.PodMonitoringResource()),
		admission.WithCustomDefaulter(&monitoringv1.PodMonitoring{}, &podMonitoringDefaulter{}),
	)
	s.Register(
		defaultPath(monitoringv1.ClusterPodMonitoringResource()),
		admission.WithCustomDefaulter(&monitoringv1.ClusterPodMonitoring{}, &clusterPodMonitoringDefaulter{}),
	)
	return nil
}

// Run the reconciliation loop of the operator.
// The passed owner references are set on cluster-wide resources created by the
// operator.
func (o *Operator) Run(ctx context.Context) error {
	defer runtimeutil.HandleCrash()

	if err := o.setupAdmissionWebhooks(ctx); err != nil {
		return errors.Wrap(err, "init admission resources")
	}
	if err := setupCollectionControllers(o); err != nil {
		return errors.Wrap(err, "setup collection controllers")
	}
	if err := setupRulesControllers(o); err != nil {
		return errors.Wrap(err, "setup rules controllers")
	}
	if err := setupOperatorConfigControllers(o); err != nil {
		return errors.Wrap(err, "setup rule-evaluator controllers")
	}

	o.logger.Info("starting GMP operator")

	return o.manager.Start(ctx)
}

// ensureCerts writes the cert/key files to the specified directory.
// If cert/key are not avalilable, generate them.
func (o *Operator) ensureCerts(ctx context.Context, dir string) ([]byte, error) {
	var (
		crt, key, caData []byte
		err              error
	)
	if o.opts.TLSKey != "" && o.opts.TLSCert != "" {
		crt, err = base64.StdEncoding.DecodeString(o.opts.TLSCert)
		if err != nil {
			return nil, errors.Wrap(err, "decoding TLS certificate")
		}
		key, err = base64.StdEncoding.DecodeString(o.opts.TLSKey)
		if err != nil {
			return nil, errors.Wrap(err, "decoding TLS key")
		}
		if o.opts.CACert != "" {
			caData, err = base64.StdEncoding.DecodeString(o.opts.CACert)
			if err != nil {
				return nil, errors.Wrap(err, "decoding certificate authority")
			}
		}
	} else if o.opts.TLSKey == "" && o.opts.TLSCert == "" && o.opts.CACert == "" {
		// Generate a self-signed pair if none was explicitly provided. It will be valid
		// for 1 year.
		// TODO(freinartz): re-generate at runtime and update the ValidatingWebhookConfiguration
		// at runtime whenever the files change.
		fqdn := fmt.Sprintf("%s.%s.svc", NameOperator, o.opts.OperatorNamespace)

		crt, key, err = cert.GenerateSelfSignedCertKey(fqdn, nil, nil)
		if err != nil {
			return nil, errors.Wrap(err, "generate self-signed TLS key pair")
		}
		// Use crt as the ca in the the self-sign case.
		caData = crt
	} else {
		return nil, errors.Errorf("Flags key-base64 and cert-base64 must both be set.")
	}
	// Create cert/key files.
	if err := ioutil.WriteFile(filepath.Join(dir, "tls.crt"), crt, 0666); err != nil {
		return nil, errors.Wrap(err, "create cert file")
	}
	if err := ioutil.WriteFile(filepath.Join(dir, "tls.key"), key, 0666); err != nil {
		return nil, errors.Wrap(err, "create key file")
	}
	return caData, nil
}

// namespacedNamePredicate is an event filter predicate that only allows events with
// a single object.
type namespacedNamePredicate struct {
	namespace string
	name      string
}

func (o namespacedNamePredicate) Create(e event.CreateEvent) bool {
	return e.Object.GetNamespace() == o.namespace && e.Object.GetName() == o.name
}
func (o namespacedNamePredicate) Update(e event.UpdateEvent) bool {
	return e.ObjectNew.GetNamespace() == o.namespace && e.ObjectNew.GetName() == o.name
}
func (o namespacedNamePredicate) Delete(e event.DeleteEvent) bool {
	return e.Object.GetNamespace() == o.namespace && e.Object.GetName() == o.name
}
func (o namespacedNamePredicate) Generic(e event.GenericEvent) bool {
	return e.Object.GetNamespace() == o.namespace && e.Object.GetName() == o.name
}

// enqueueConst always enqueues the same request regardless of the event.
type enqueueConst reconcile.Request

func (e enqueueConst) Create(_ event.CreateEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request(e))
}

func (e enqueueConst) Update(_ event.UpdateEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request(e))
}

func (e enqueueConst) Delete(_ event.DeleteEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request(e))
}

func (e enqueueConst) Generic(_ event.GenericEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request(e))
}

func validatePath(gvr metav1.GroupVersionResource) string {
	return fmt.Sprintf("/validate/%s/%s/%s", gvr.Group, gvr.Version, gvr.Resource)
}

func defaultPath(gvr metav1.GroupVersionResource) string {
	return fmt.Sprintf("/default/%s/%s/%s", gvr.Group, gvr.Version, gvr.Resource)
}

func (o *Operator) webhookConfigName() string {
	return fmt.Sprintf("%s.%s.monitoring.googleapis.com", NameOperator, o.opts.OperatorNamespace)
}

func (o *Operator) setValidatingWebhookCABundle(ctx context.Context, caBundle []byte) error {
	var vwc arv1.ValidatingWebhookConfiguration
	err := o.client.Get(ctx, client.ObjectKey{Name: o.webhookConfigName()}, &vwc)
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	for i := range vwc.Webhooks {
		vwc.Webhooks[i].ClientConfig.CABundle = caBundle
	}
	return o.client.Update(ctx, &vwc)
}

func (o *Operator) setMutatingWebhookCABundle(ctx context.Context, caBundle []byte) error {
	var mwc arv1.MutatingWebhookConfiguration
	err := o.client.Get(ctx, client.ObjectKey{Name: o.webhookConfigName()}, &mwc)
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	for i := range mwc.Webhooks {
		mwc.Webhooks[i].ClientConfig.CABundle = caBundle
	}
	return o.client.Update(ctx, &mwc)
}

func minimalSecurityContext() *corev1.SecurityContext {
	id := int64(1000)
	t := true
	f := false

	return &corev1.SecurityContext{
		RunAsUser:                &id,
		RunAsGroup:               &id,
		RunAsNonRoot:             &t,
		Privileged:               &f,
		AllowPrivilegeEscalation: &f,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"all"},
		},
	}
}
func podSpecSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}
