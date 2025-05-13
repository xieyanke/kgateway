package setup_test

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/grpclog"
	istiokube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test/util/retry"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/settings"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/proxy_syncer"
	"github.com/kgateway-dev/kgateway/v2/test/envtestutil"
)

func getAssetsDir(t *testing.T) string {
	var assets string
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		// set default if not user provided
		out, err := exec.Command("sh", "-c", "make -sC $(dirname $(go env GOMOD)) envtest-path").CombinedOutput()
		t.Log("out:", string(out))
		if err != nil {
			t.Fatalf("failed to get assets dir: %v", err)
		}
		assets = strings.TrimSpace(string(out))
	}
	return assets
}

// testingWriter is a WriteSyncer that writes logs to testing.T.
type testingWriter struct {
	t atomic.Value
}

func (w *testingWriter) Write(p []byte) (n int, err error) {
	w.t.Load().(*testing.T).Log(string(p)) // Write the log to testing.T
	return len(p), nil
}

func (w *testingWriter) Sync() error {
	return nil
}

func (w *testingWriter) set(t *testing.T) {
	w.t.Store(t)
}

var (
	writer = &testingWriter{}
	logger = NewTestLogger()
)

// NewTestLogger creates a zap.Logger which can be used to write to *testing.T
// on each test, set the *testing.T on the writer.
func NewTestLogger() *zap.Logger {
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.AddSync(writer),
		// Adjust log level as needed
		// if a test assertion fails and logs or too noisy, change to zapcore.FatalLevel
		zapcore.DebugLevel,
	)

	return zap.New(core, zap.AddCaller())
}

func init() {
	log.SetLogger(zapr.NewLogger(logger))
	grpclog.SetLoggerV2(grpclog.NewLoggerV2WithVerbosity(writer, writer, writer, 100))
}

func TestServiceEntry(t *testing.T) {
	t.Run("no DR plugin", func(t *testing.T) {
		st, err := settings.BuildSettings()
		if err != nil {
			t.Fatalf("can't get settings %v", err)
		}
		st.EnableIstioIntegration = false
		runScenario(t, "testdata/serviceentry", st)
	})

	t.Run("DR plugin enabled", func(t *testing.T) {
		st, err := settings.BuildSettings()
		if err != nil {
			t.Fatalf("can't get settings %v", err)
		}
		st.EnableIstioIntegration = true

		// // we can re-run these with the plugin on and expect nothing to change
		// runScenario(t, "testdata/serviceentry", st)

		// these exercise applying a DR to a ServiceEntry
		runScenario(t, "testdata/serviceentry/dr", st)
	})
}

func TestDestinationRule(t *testing.T) {
	st, err := settings.BuildSettings()
	st.EnableIstioIntegration = true
	if err != nil {
		t.Fatalf("can't get settings %v", err)
	}
	runScenario(t, "testdata/istio_destination_rule", st)
}

func TestWithStandardSettings(t *testing.T) {
	st, err := settings.BuildSettings()
	if err != nil {
		t.Fatalf("can't get settings %v", err)
	}
	runScenario(t, "testdata/standard", st)
}

func TestWithIstioAutomtlsSettings(t *testing.T) {
	st, err := settings.BuildSettings()
	st.EnableIstioIntegration = true
	st.EnableIstioAutoMtls = true
	if err != nil {
		t.Fatalf("can't get settings %v", err)
	}
	runScenario(t, "testdata/istio_mtls", st)
}

func TestWithAutoDns(t *testing.T) {
	st, err := settings.BuildSettings()
	if err != nil {
		t.Fatalf("can't get settings %v", err)
	}
	st.DnsLookupFamily = "AUTO"

	runScenario(t, "testdata/autodns", st)
}

func TestWithInferenceAPI(t *testing.T) {
	st, err := settings.BuildSettings()
	if err != nil {
		t.Fatalf("can't get settings %v", err)
	}
	st.EnableInferExt = true
	st.InferExtAutoProvision = true

	runScenario(t, "testdata/inference_api", st)
}

func TestPolicyUpdate(t *testing.T) {
	st, err := settings.BuildSettings()
	if err != nil {
		t.Fatalf("can't get settings %v", err)
	}
	setupEnvTestAndRun(t, st, func(t *testing.T, ctx context.Context, kdbg *krt.DebugHandler, client istiokube.CLIClient, xdsPort int) {
		client.Kube().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gwtest"}}, metav1.CreateOptions{})

		err = client.ApplyYAMLContents("gwtest", `kind: Gateway
apiVersion: gateway.networking.k8s.io/v1
metadata:
  name: http-gw
  namespace: gwtest
spec:
  gatewayClassName: kgateway
  listeners:
  - protocol: HTTP
    port: 8080
    name: http
    allowedRoutes:
      namespaces:
        from: All`, `apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: transformation
  namespace: gwtest
spec:
  transformation:
    response:
      set:
      - name: x-solo-response
        value: '{{ request_header("x-solo-request") }}'
      remove:
      - x-solo-request`, `apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: happypath
  namespace: gwtest
spec:
  parentRefs:
    - name: http-gw
  hostnames:
    - "www.example2.com"
  rules:
    - backendRefs:
        - name: kubernetes
          port: 443
      filters:
      - type: ExtensionRef
        extensionRef:
          group: gateway.kgateway.dev
          kind: TrafficPolicy
          name: transformation`)

		time.Sleep(time.Second / 2)

		err = client.ApplyYAMLContents("gwtest", `apiVersion: gateway.kgateway.dev/v1alpha1
kind: TrafficPolicy
metadata:
  name: transformation
  namespace: gwtest
spec:
  transformation:
    response:
      set:
      - name: x-solo-response
        value: '{{ request_header("x-solo-request123") }}'
      remove:
      - x-solo-request321`)

		time.Sleep(time.Second / 2)

		dumper := envtestutil.NewXdsDumper(t, ctx, envtestutil.WithXdsPort(xdsPort), envtestutil.WithGwname("http-gw"))
		t.Cleanup(dumper.Close)
		t.Cleanup(func() {
			if t.Failed() {
				logKrtState(t, fmt.Sprintf("krt state for failed test: %s", t.Name()), kdbg)
			} else if os.Getenv("KGW_DUMP_KRT_ON_SUCCESS") == "true" {
				logKrtState(t, fmt.Sprintf("krt state for successful test: %s", t.Name()), kdbg)
			}
		})

		dump, err := dumper.Dump(t, ctx)
		if err != nil {
			t.Error(err)
		}
		pfc := dump.Routes[0].GetVirtualHosts()[0].GetRoutes()[0].GetTypedPerFilterConfig()
		if len(pfc) != 1 {
			t.Fatalf("expected 1 filter config, got %d", len(pfc))
		}
		if !bytes.Contains(slices.Collect(maps.Values(pfc))[0].Value, []byte("x-solo-request321")) {
			t.Fatalf("expected filter config to contain x-solo-request321")
		}

		t.Logf("%s finished", t.Name())
	})
}

func runScenario(t *testing.T, scenarioDir string, globalSettings *settings.Settings) {
	setupEnvTestAndRun(t, globalSettings, func(t *testing.T, ctx context.Context, kdbg *krt.DebugHandler, client istiokube.CLIClient, xdsPort int) {
		// list all yamls in test data
		files, err := os.ReadDir(scenarioDir)
		if err != nil {
			t.Fatalf("failed to read dir: %v", err)
		}
		for _, f := range files {
			// run tests with the yaml files (but not -out.yaml files)/s
			parentT := t
			if strings.HasSuffix(f.Name(), ".yaml") && !strings.HasSuffix(f.Name(), "-out.yaml") {
				if os.Getenv("TEST_PREFIX") != "" && !strings.HasPrefix(f.Name(), os.Getenv("TEST_PREFIX")) {
					continue
				}
				fullpath := filepath.Join(scenarioDir, f.Name())
				t.Run(strings.TrimSuffix(f.Name(), ".yaml"), func(t *testing.T) {
					writer.set(t)
					t.Cleanup(func() {
						writer.set(parentT)
					})
					// sadly tests can't run yet in parallel, as kgateway will add all the k8s services as clusters. this means
					// that we get test pollution.
					// once we change it to only include the ones in the proxy, we can re-enable this
					//				t.Parallel()
					testScenario(t, ctx, kdbg, client, xdsPort, fullpath)
				})
			}
		}
	})
}

func setupEnvTestAndRun(t *testing.T, globalSettings *settings.Settings, run func(t *testing.T,
	ctx context.Context,
	kdbg *krt.DebugHandler,
	client istiokube.CLIClient,
	xdsPort int,
),
) {
	proxy_syncer.UseDetailedUnmarshalling = true
	writer.set(t)

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "crds"),
			filepath.Join("..", "..", "..", "install", "helm", "kgateway-crds", "templates"),
			filepath.Join("testdata", "istio_crds_setup"),
		},
		ErrorIfCRDPathMissing: true,
		// set assets dir so we can run without the makefile
		BinaryAssetsDirectory: getAssetsDir(t),
		// web hook to add cluster ips to services
	}
	envtestutil.RunController(t, logger, globalSettings, testEnv,
		[][]string{
			[]string{"default", "testdata/setup_yaml/setup.yaml"},
			[]string{"gwtest", "testdata/setup_yaml/pods.yaml"},
		},
		run)
}

func testScenario(
	t *testing.T,
	ctx context.Context,
	kdbg *krt.DebugHandler,
	client istiokube.CLIClient,
	xdsPort int,
	f string,
) {
	fext := filepath.Ext(f)
	fpre := strings.TrimSuffix(f, fext)
	t.Logf("running scenario for test file: %s", f)

	// read the out file
	fout := fpre + "-out" + fext
	write := false
	ya, err := os.ReadFile(fout)
	// if not exist
	if os.IsNotExist(err) {
		write = true
		err = nil
	}
	if os.Getenv("REFRESH_GOLDEN") == "true" {
		write = true
	}
	if err != nil {
		t.Fatalf("failed to read file %s: %v", fout, err)
	}

	var expectedXdsDump envtestutil.XdsDump
	err = expectedXdsDump.FromYaml(ya)
	if err != nil {
		t.Fatalf("failed to read yaml %s: %v", fout, err)
	}
	const gwname = "http-gw-for-test"
	testgwname := "http-" + filepath.Base(fpre)
	testyamlbytes, err := os.ReadFile(f)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	// change the gw name, so we could potentially run multiple tests in parallel (tough currently
	// it has other issues, so we don't run them in parallel)
	testyaml := strings.ReplaceAll(string(testyamlbytes), gwname, testgwname)

	yamlfile := filepath.Join(t.TempDir(), "test.yaml")
	os.WriteFile(yamlfile, []byte(testyaml), 0o644)

	err = client.ApplyYAMLFiles("", yamlfile)

	t.Cleanup(func() {
		// always delete yamls, even if there was an error applying them; to prevent test pollution.
		err := client.DeleteYAMLFiles("", yamlfile)
		if err != nil {
			t.Fatalf("failed to delete yaml: %v", err)
		}
		t.Log("deleted yamls", t.Name())
	})

	if err != nil {
		t.Fatalf("failed to apply yaml: %v", err)
	}
	t.Log("applied yamls", t.Name())

	// wait at least a second before the first check
	// to give the CP time to process
	time.Sleep(time.Second)

	t.Cleanup(func() {
		if t.Failed() {
			logKrtState(t, fmt.Sprintf("krt state for failed test: %s", t.Name()), kdbg)
		} else if os.Getenv("KGW_DUMP_KRT_ON_SUCCESS") == "true" {
			logKrtState(t, fmt.Sprintf("krt state for successful test: %s", t.Name()), kdbg)
		}
	})

	retry.UntilSuccessOrFail(t, func() error {
		dumper := envtestutil.NewXdsDumper(t, ctx, envtestutil.WithXdsPort(xdsPort), envtestutil.WithGwname(testgwname))
		defer dumper.Close()
		dump, err := dumper.Dump(t, ctx)
		if err != nil {
			return err
		}
		if len(dump.Listeners) == 0 {
			return fmt.Errorf("timed out waiting for listeners")
		}
		if write {
			t.Logf("writing out file")
			// serialize xdsDump to yaml
			d, err := dump.ToYaml()
			if err != nil {
				return fmt.Errorf("failed to serialize xdsDump: %v", err)
			}
			os.WriteFile(fout, d, 0o644)
			return fmt.Errorf("wrote out file - nothing to test")
		}
		return dump.Compare(expectedXdsDump)
	}, retry.Converge(2), retry.BackoffDelay(2*time.Second), retry.Timeout(10*time.Second))
	t.Logf("%s finished", t.Name())
}

// logKrtState logs the krt state with a message
func logKrtState(t *testing.T, msg string, kdbg *krt.DebugHandler) {
	t.Helper()
	j, err := kdbg.MarshalJSON()
	if err != nil {
		t.Logf("failed to marshal krt state: %v", err)
	} else {
		t.Logf("%s: %s", msg, string(j))
	}
}
