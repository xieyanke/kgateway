# Setup
Get gw running from source:

```sh
ctlptl create cluster kind --name kind-kgateway --registry=ctlptl-registry
VERSION=1.0.0-ci1 CLUSTER_NAME=kgateway IMAGE_VARIANT=standard make kind-build-and-load
tilt up
```

# Baseline
Apply a gateway and 1000 HTTPRoutes and Backend resources:

```sh
kubectl apply -f gw.yaml
go run github.com/solo-io/scale-testing/applier@ebaf6906eb26c96a3eee96bb7b435aa4f282e837 apply -f routes.yaml --iterations 1000
```

(Note that github.com/solo-io/scale-testing is private, so you need to setup git access for this to work.)

# Port-forward
```sh
kubectl port-forward deployment/http 8080&
kubectl port-forward deployment/http 19000&
```

# Test!

Note that `curl localhost:8080/foo/1000` will return 404 because the route and backend are not there.

To test the translation sleep, add one more route!

```
go run github.com/solo-io/scale-testing/applier@ebaf6906eb26c96a3eee96bb7b435aa4f282e837 apply -f routes.yaml --start 1000 --iterations 1
```

and curl again and see a response from httpbin.io pretty much immediately. This indicates that the translation from user object to envoy happened very fast.

In my testing, the kagent binary consumed around 100MB of RAM, and 0% cpu on steady state.