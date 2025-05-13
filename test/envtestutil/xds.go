package envtestutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	envoycluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoycore "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyendpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoylistener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_config_route_v3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	envoyhttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discovery_v3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	jsonpb "google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
	istioslices "istio.io/istio/pkg/slices"
	"sigs.k8s.io/yaml"
)

type xdsDumpOptions struct {
	xdsPort int
	gwname  string
	gwns    string
}

type XdsDumpOption func(*xdsDumpOptions)

func WithXdsPort(xdsPort int) XdsDumpOption {
	return func(x *xdsDumpOptions) {
		x.xdsPort = xdsPort
	}
}

func WithGwname(gwname string) XdsDumpOption {
	return func(x *xdsDumpOptions) {
		x.gwname = gwname
	}
}

func WithGwns(gwns string) XdsDumpOption {
	return func(x *xdsDumpOptions) {
		x.gwns = gwns
	}
}

type xdsDumper struct {
	conn      *grpc.ClientConn
	adsClient discovery_v3.AggregatedDiscoveryService_StreamAggregatedResourcesClient
	dr        *discovery_v3.DiscoveryRequest
	cancel    context.CancelFunc
}

func (x xdsDumper) Close() {
	if x.conn != nil {
		x.conn.Close()
	}
	if x.adsClient != nil {
		x.adsClient.CloseSend()
	}
	if x.cancel != nil {
		x.cancel()
	}
}

func NewXdsDumper(t *testing.T, ctx context.Context, opts ...XdsDumpOption) xdsDumper {
	var options xdsDumpOptions
	options.gwns = "gwtest"
	for _, opt := range opts {
		opt(&options)
	}
	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", options.xdsPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithIdleTimeout(time.Second*10),
	)
	if err != nil {
		if t != nil {
			t.Fatalf("failed to connect to xds server: %v", err)
		} else {
			panic(err)
		}
	}

	d := xdsDumper{
		conn: conn,
		dr: &discovery_v3.DiscoveryRequest{Node: &envoycore.Node{
			Id: "gateway.gwtest",
			Metadata: &structpb.Struct{
				Fields: map[string]*structpb.Value{"role": {Kind: &structpb.Value_StringValue{StringValue: fmt.Sprintf("kgateway-kube-gateway-api~%s~%s", options.gwns, options.gwname)}}},
			},
		}},
	}

	ads := discovery_v3.NewAggregatedDiscoveryServiceClient(d.conn)
	ctx, cancel := context.WithTimeout(ctx, time.Second*30) // long timeout - just in case. we should never reach it.
	adsClient, err := ads.StreamAggregatedResources(ctx)
	if err != nil {
		if t != nil {
			t.Fatalf("failed to get ads client: %v", err)
		} else {
			panic(err)
		}
	}
	d.adsClient = adsClient
	d.cancel = cancel

	return d
}

func (x xdsDumper) Dump(t *testing.T, ctx context.Context) (XdsDump, error) {
	dr := proto.Clone(x.dr).(*discovery_v3.DiscoveryRequest)
	dr.TypeUrl = "type.googleapis.com/envoy.config.cluster.v3.Cluster"
	x.adsClient.Send(dr)
	dr = proto.Clone(x.dr).(*discovery_v3.DiscoveryRequest)
	dr.TypeUrl = "type.googleapis.com/envoy.config.listener.v3.Listener"
	x.adsClient.Send(dr)

	var clusters []*envoycluster.Cluster
	var listeners []*envoylistener.Listener
	var errs error
	var logf func(format string, args ...any)
	if t != nil {
		logf = t.Logf
	} else {
		logf = func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}
	}

	// run this in parallel with a 5s timeout
	done := make(chan struct{})
	go func() {
		defer close(done)
		sent := 2
		for i := 0; i < sent; i++ {
			dresp, err := x.adsClient.Recv()
			if err != nil {
				errs = errors.Join(errs, fmt.Errorf("failed to get response from xds server: %v", err))
			}
			logf("got response: %s len: %d", dresp.GetTypeUrl(), len(dresp.GetResources()))
			if dresp.GetTypeUrl() == "type.googleapis.com/envoy.config.cluster.v3.Cluster" {
				for _, anyCluster := range dresp.GetResources() {
					var cluster envoycluster.Cluster
					if err := anyCluster.UnmarshalTo(&cluster); err != nil {
						errs = errors.Join(errs, fmt.Errorf("failed to unmarshal cluster: %v", err))
					}
					clusters = append(clusters, &cluster)
				}
			} else if dresp.GetTypeUrl() == "type.googleapis.com/envoy.config.listener.v3.Listener" {
				needMoreListerners := false
				for _, anyListener := range dresp.GetResources() {
					var listener envoylistener.Listener
					if err := anyListener.UnmarshalTo(&listener); err != nil {
						errs = errors.Join(errs, fmt.Errorf("failed to unmarshal listener: %v", err))
					}
					listeners = append(listeners, &listener)
					needMoreListerners = needMoreListerners || (len(getroutesnames(&listener)) == 0)
				}
				if len(listeners) == 0 {
					needMoreListerners = true
				}

				if needMoreListerners {
					// no routes on listener.. request another listener snapshot, after
					// the control plane processes the listeners
					sent += 1
					listeners = nil
					dr = proto.Clone(x.dr).(*discovery_v3.DiscoveryRequest)
					dr.TypeUrl = "type.googleapis.com/envoy.config.listener.v3.Listener"
					dr.VersionInfo = dresp.GetVersionInfo()
					dr.ResponseNonce = dresp.GetNonce()
					x.adsClient.Send(dr)
				}
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// don't fatal yet as we want to dump the state while still connected
		errs = errors.Join(errs, fmt.Errorf("timed out waiting for listener/cluster xds dump"))
		return XdsDump{}, errs
	}
	if len(listeners) == 0 {
		errs = errors.Join(errs, fmt.Errorf("no listeners found"))
		return XdsDump{}, errs
	}
	logf("xds: found %d listeners and %d clusters", len(listeners), len(clusters))

	clusterServiceNames := istioslices.MapFilter(clusters, func(c *envoycluster.Cluster) *string {
		if c.GetEdsClusterConfig() != nil {
			if c.GetEdsClusterConfig().GetServiceName() != "" {
				s := c.GetEdsClusterConfig().GetServiceName()
				if s == "" {
					s = c.GetName()
				}
				return &s
			}
			return &c.Name
		}
		return nil
	})

	var routenames []string
	for _, l := range listeners {
		routenames = append(routenames, getroutesnames(l)...)
	}

	dr = proto.Clone(x.dr).(*discovery_v3.DiscoveryRequest)
	dr.ResourceNames = routenames
	dr.TypeUrl = "type.googleapis.com/envoy.config.route.v3.RouteConfiguration"
	x.adsClient.Send(dr)
	dr = proto.Clone(x.dr).(*discovery_v3.DiscoveryRequest)
	dr.TypeUrl = "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment"
	dr.ResourceNames = clusterServiceNames
	x.adsClient.Send(dr)

	var endpoints []*envoyendpoint.ClusterLoadAssignment
	var routes []*envoy_config_route_v3.RouteConfiguration

	done = make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			dresp, err := x.adsClient.Recv()
			if err != nil {
				errs = errors.Join(errs, fmt.Errorf("failed to get response from xds server: %v", err))
			}
			logf("got response: %s len: %d", dresp.GetTypeUrl(), len(dresp.GetResources()))
			if dresp.GetTypeUrl() == "type.googleapis.com/envoy.config.route.v3.RouteConfiguration" {
				for _, anyRoute := range dresp.GetResources() {
					var route envoy_config_route_v3.RouteConfiguration
					if err := anyRoute.UnmarshalTo(&route); err != nil {
						errs = errors.Join(errs, fmt.Errorf("failed to unmarshal route: %v", err))
					}
					routes = append(routes, &route)
				}
			} else if dresp.GetTypeUrl() == "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment" {
				for _, anyCla := range dresp.GetResources() {
					var cla envoyendpoint.ClusterLoadAssignment
					if err := anyCla.UnmarshalTo(&cla); err != nil {
						errs = errors.Join(errs, fmt.Errorf("failed to unmarshal cla: %v", err))
					}
					// remove kube endpoints, as with envtests we will get random ports, so we cant assert on them
					if !strings.Contains(cla.ClusterName, "kubernetes") {
						endpoints = append(endpoints, &cla)
					}
				}
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// don't fatal yet as we want to dump the state while still connected
		errs = errors.Join(errs, fmt.Errorf("timed out waiting for routes/cla xds dump"))
		return XdsDump{}, errs
	}

	logf("found %d routes and %d endpoints", len(routes), len(endpoints))
	xdsDump := XdsDump{
		Clusters:  clusters,
		Listeners: listeners,
		Endpoints: endpoints,
		Routes:    routes,
	}
	return xdsDump, errs
}

type XdsDump struct {
	Clusters  []*envoycluster.Cluster
	Listeners []*envoylistener.Listener
	Endpoints []*envoyendpoint.ClusterLoadAssignment
	Routes    []*envoy_config_route_v3.RouteConfiguration
}

func (x *XdsDump) Compare(other XdsDump) error {
	var errs error

	if len(x.Clusters) != len(other.Clusters) {
		errs = errors.Join(errs, fmt.Errorf("expected %v clusters, got %v", len(other.Clusters), len(x.Clusters)))
	}

	if len(x.Listeners) != len(other.Listeners) {
		errs = errors.Join(errs, fmt.Errorf("expected %v listeners, got %v", len(other.Listeners), len(x.Listeners)))
	}
	if len(x.Endpoints) != len(other.Endpoints) {
		errs = errors.Join(errs, fmt.Errorf("expected %v endpoints, got %v", len(other.Endpoints), len(x.Endpoints)))
	}
	if len(x.Routes) != len(other.Routes) {
		errs = errors.Join(errs, fmt.Errorf("expected %v routes, got %v", len(other.Routes), len(x.Routes)))
	}

	clusterset := map[string]*envoycluster.Cluster{}
	for _, c := range x.Clusters {
		clusterset[c.Name] = c
	}
	for _, otherc := range other.Clusters {
		ourc := clusterset[otherc.Name]
		if ourc == nil {
			errs = errors.Join(errs, fmt.Errorf("cluster %v not found", otherc.Name))
			continue
		}
		ourCla := ourc.LoadAssignment
		otherCla := otherc.LoadAssignment
		if err := compareCla(ourCla, otherCla); err != nil {
			errs = errors.Join(errs, fmt.Errorf("cluster %v: %w", otherc.Name, err))
		}

		// don't proto.Equal the LoadAssignment
		ourc.LoadAssignment = nil
		otherc.LoadAssignment = nil
		if !proto.Equal(otherc, ourc) {
			errs = errors.Join(errs, fmt.Errorf("cluster %v not equal: got: %s, expected: %s", otherc.Name, ourc.String(), otherc.String()))
		}
		ourc.LoadAssignment = ourCla
		otherc.LoadAssignment = otherCla
	}
	listenerset := map[string]*envoylistener.Listener{}
	for _, c := range x.Listeners {
		listenerset[c.Name] = c
	}
	for _, c := range other.Listeners {
		otherc := listenerset[c.Name]
		if otherc == nil {
			errs = errors.Join(errs, fmt.Errorf("listener %v not found", c.Name))
			continue
		}
		if !proto.Equal(c, otherc) {
			errs = errors.Join(errs, fmt.Errorf("listener %v not equal", c.Name))
		}
	}
	routeset := map[string]*envoy_config_route_v3.RouteConfiguration{}
	for _, c := range x.Routes {
		routeset[c.Name] = c
	}
	for _, c := range other.Routes {
		otherc := routeset[c.Name]
		if otherc == nil {
			errs = errors.Join(errs, fmt.Errorf("route %v not found", c.Name))
			continue
		}

		// Ignore VirtualHost ordering
		vhostFn := func(x, y *envoy_config_route_v3.VirtualHost) bool { return x.Name < y.Name }
		if diff := cmp.Diff(c, otherc, protocmp.Transform(),
			protocmp.SortRepeated(vhostFn)); diff != "" {
			errs = errors.Join(errs, fmt.Errorf("route %v not equal!\ndiff:\b%s\n", c.Name, diff))
		}
	}

	epset := map[string]*envoyendpoint.ClusterLoadAssignment{}
	for _, c := range x.Endpoints {
		epset[c.ClusterName] = c
	}
	for _, c := range other.Endpoints {
		otherc := epset[c.ClusterName]
		if err := compareCla(c, otherc); err != nil {
			errs = errors.Join(errs, fmt.Errorf("endpoint %v: %w", c.ClusterName, err))
		}
	}

	return errs
}

func compareCla(c, otherc *envoyendpoint.ClusterLoadAssignment) error {
	if (c == nil) != (otherc == nil) {
		if c == nil {
			return fmt.Errorf("cluster is nil")
		}
		return fmt.Errorf("ep %v not found", c.ClusterName)
	}
	if c == nil || otherc == nil {
		return nil
	}
	ep1 := flattenendpoints(c)
	ep2 := flattenendpoints(otherc)
	if !equalset(ep1, ep2) {
		return fmt.Errorf("ep list %v not equal: %v %v", c.ClusterName, ep1, ep2)
	}
	ce := c.Endpoints
	ocd := otherc.Endpoints
	c.Endpoints = nil
	otherc.Endpoints = nil
	if !proto.Equal(c, otherc) {
		return fmt.Errorf("ep %v not equal", c.ClusterName)
	}
	c.Endpoints = ce
	otherc.Endpoints = ocd
	return nil
}

func equalset(a, b []*envoyendpoint.LocalityLbEndpoints) bool {
	if len(a) != len(b) {
		return false
	}
	for _, v := range a {
		if istioslices.FindFunc(b, func(e *envoyendpoint.LocalityLbEndpoints) bool {
			return proto.Equal(v, e)
		}) == nil {
			return false
		}
	}
	return true
}

func flattenendpoints(v *envoyendpoint.ClusterLoadAssignment) []*envoyendpoint.LocalityLbEndpoints {
	var flat []*envoyendpoint.LocalityLbEndpoints
	for _, e := range v.Endpoints {
		for _, l := range e.LbEndpoints {
			flatbase := proto.Clone(e).(*envoyendpoint.LocalityLbEndpoints)
			flatbase.LbEndpoints = []*envoyendpoint.LbEndpoint{l}
			flat = append(flat, flatbase)
		}
	}
	return flat
}

func (x *XdsDump) FromYaml(ya []byte) error {
	ya, err := yaml.YAMLToJSON(ya)
	if err != nil {
		return err
	}

	jsonM := map[string][]any{}
	err = json.Unmarshal(ya, &jsonM)
	if err != nil {
		return err
	}
	for _, c := range jsonM["clusters"] {
		r, err := anyJsonRoundTrip[envoycluster.Cluster](c)
		if err != nil {
			return err
		}
		x.Clusters = append(x.Clusters, r)
	}
	for _, c := range jsonM["endpoints"] {
		r, err := anyJsonRoundTrip[envoyendpoint.ClusterLoadAssignment](c)
		if err != nil {
			return err
		}
		x.Endpoints = append(x.Endpoints, r)
	}
	for _, c := range jsonM["listeners"] {
		r, err := anyJsonRoundTrip[envoylistener.Listener](c)
		if err != nil {
			return err
		}
		x.Listeners = append(x.Listeners, r)
	}
	for _, c := range jsonM["routes"] {
		r, err := anyJsonRoundTrip[envoy_config_route_v3.RouteConfiguration](c)
		if err != nil {
			return err
		}
		x.Routes = append(x.Routes, r)
	}
	return nil
}

func anyJsonRoundTrip[T any, PT interface {
	proto.Message
	*T
}](c any) (PT, error) {
	var ju jsonpb.UnmarshalOptions
	jb, err := json.Marshal(c)
	var zero PT
	if err != nil {
		return zero, err
	}
	var r T
	var pr PT = &r
	err = ju.Unmarshal(jb, pr)
	return pr, err
}

func sortResource[T fmt.Stringer](resources []T) []T {
	// clone the slice
	resources = append([]T(nil), resources...)
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].String() < resources[j].String()
	})
	return resources
}

func (x *XdsDump) ToYaml() ([]byte, error) {
	jsonM := map[string][]any{}
	for _, c := range sortResource(x.Clusters) {
		roundtrip, err := protoJsonRoundTrip(c)
		if err != nil {
			return nil, err
		}
		jsonM["clusters"] = append(jsonM["clusters"], roundtrip)
	}
	for _, c := range sortResource(x.Listeners) {
		roundtrip, err := protoJsonRoundTrip(c)
		if err != nil {
			return nil, err
		}
		jsonM["listeners"] = append(jsonM["listeners"], roundtrip)
	}
	for _, c := range sortResource(x.Endpoints) {
		roundtrip, err := protoJsonRoundTrip(c)
		if err != nil {
			return nil, err
		}
		jsonM["endpoints"] = append(jsonM["endpoints"], roundtrip)
	}
	for _, c := range sortResource(x.Routes) {
		roundtrip, err := protoJsonRoundTrip(c)
		if err != nil {
			return nil, err
		}
		jsonM["routes"] = append(jsonM["routes"], roundtrip)
	}

	bytes, err := json.Marshal(jsonM)
	if err != nil {
		return nil, err
	}

	ya, err := yaml.JSONToYAML(bytes)
	if err != nil {
		return nil, err
	}
	return ya, nil
}

func protoJsonRoundTrip(c proto.Message) (any, error) {
	var j jsonpb.MarshalOptions
	s, err := j.Marshal(c)
	if err != nil {
		return nil, err
	}
	var roundtrip any
	err = json.Unmarshal(s, &roundtrip)
	if err != nil {
		return nil, err
	}
	return roundtrip, nil
}

func getroutesnames(l *envoylistener.Listener) []string {
	var routes []string
	for _, fc := range l.GetFilterChains() {
		for _, filter := range fc.GetFilters() {
			suffix := string((&envoyhttp.HttpConnectionManager{}).ProtoReflect().Descriptor().FullName())
			if strings.HasSuffix(filter.GetTypedConfig().GetTypeUrl(), suffix) {
				var hcm envoyhttp.HttpConnectionManager
				switch config := filter.GetConfigType().(type) {
				case *envoylistener.Filter_TypedConfig:
					if err := config.TypedConfig.UnmarshalTo(&hcm); err == nil {
						rds := hcm.GetRds().GetRouteConfigName()
						if rds != "" {
							routes = append(routes, rds)
						}
					}
				}
			}
		}
	}
	return routes
}
