# RHOSSM: Istio Multicluster-Multiprimary (vSphere with MetalLB)

The same cluster domain is used in both clusters.

## Prerequisites
- The same root of trust must be used in this use case. For this, follow the Istio [guide](https://istio.io/latest/docs/tasks/security/cert-management/plugin-ca-cert/).
- MetalLB installed.

## Installing Istio Multicluster/Multiprimary
Create OCP project:
```bash
oc new-project istio-system
```

- Cluster1
```bash
oc label namespace istio-system topology.istio.io/network=cluster1-network
```

- Cluster2
```bash
oc label namespace istio-system topology.istio.io/network=cluster2-network
```

Create CA and certificates.Open a new terminal, clone the Istio [repository](https://github.com/istio/istio) and go to _istio_ folder (new cloned repo). The steps must be executed from _istio_ folder.
```bash
mkdir certs
pushd certs
make -f ../tools/certs/Makefile.selfsigned.mk root-ca
make -f ../tools/certs/Makefile.selfsigned.mk cluster1-cacerts
make -f ../tools/certs/Makefile.selfsigned.mk cluster2-cacerts
```

Create the Istio secret in both clusters:

- Cluster1
```bash
oc create secret generic cacerts -n istio-system \
      --from-file=cluster1/ca-cert.pem \
      --from-file=cluster1/ca-key.pem \
      --from-file=cluster1/root-cert.pem \
      --from-file=cluster1/cert-chain.pem
```

- Cluster2
```bash
oc create secret generic cacerts -n istio-system \
      --from-file=cluster2/ca-cert.pem \
      --from-file=cluster2/ca-key.pem \
      --from-file=cluster2/root-cert.pem \
      --from-file=cluster2/cert-chain.pem
```

```bash
popd
```

Deploy the SMCP resource in both clusters:

- Cluster1
```bash
oc apply -f cluster1/smcp.yaml
```

- Cluster2
```bash
oc apply -f cluster2/smcp.yaml
```

Deploy the dedicated gateways for east-west traffic:

- Cluster1
```bash
oc apply -f cluster1/istio-eastwestgateway.yaml
```

- Cluster2
```bash
oc apply -f cluster2/istio-eastwestgateway.yaml
```

Generate kubeconfigs for remote clusters:

- Cluster1
```bash
./generate-kubeconfig.sh \
  --cluster-name=cluster1 \
  --namespace=istio-system \
  --revision=basic \
  --remote-kubeconfig-path=<KUBECONFIG-cluster1> > cluster2/remote-secret.yaml
```
- Cluster2
```bash
./generate-kubeconfig.sh \
  --cluster-name=cluster2 \
  --namespace=istio-system \
  --revision=basic \
  --remote-kubeconfig-path=<KUBECONFIG-cluster2> > cluster1/remote-secret.yaml
```

Create secrets from generated kubeconfig:

- Cluster1
```bash
oc create secret generic istio-remote-secret-cluster2-cluster -n istio-system --from-file=./cluster1/remote-secret.yaml --type=string
oc annotate secret istio-remote-secret-cluster2-cluster -n istio-system networking.istio.io/cluster='cluster2-cluster'
oc label secret istio-remote-secret-cluster2-cluster -n istio-system istio/multiCluster='true'
```

- Cluster2
```bash
oc create secret generic istio-remote-secret-cluster1-cluster -n istio-system --from-file=./cluster2/remote-secret.yaml --type=string
oc annotate secret istio-remote-secret-cluster1-cluster -n istio-system networking.istio.io/cluster='cluster1-cluster'
oc label secret istio-remote-secret-cluster1-cluster -n istio-system istio/multiCluster='true'
```

## Deploying sample applications
Create the _my-awesome-project_ OCP project:
```bash
oc new-project my-awesome-project
oc new-project sleep
```

Since we are using [Automatic Sidecar Injection](https://istio.io/latest/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection), label the _my-awesome-project_ OCP project:

```bash
oc label namespace my-awesome-project istio-injection=enabled
oc label namespace sleep istio-injection=enabled
```

Deploy applications:
```bash
oc apply -f apps/
oc apply -f https://raw.githubusercontent.com/maistra/istio/maistra-2.4/samples/sleep/sleep.yaml -n sleep
```

Test communication between services and meshes:
```bash
oc exec $SLEEP_POD -- -- curl -sS helloworld.my-awesome-project:5000/hello
```

Check the discovered endpoints for the _helloworld.my-awesome-project_ service:
```bash
istioctl pc endpoints $SLEEP_POD --cluster "outbound|5000||helloworld.my-awesome-project.svc.cluster.local" 

ENDPOINT                 STATUS      OUTLIER CHECK     CLUSTER
10.128.2.214:5000        HEALTHY     OK                outbound|5000||helloworld.my-awesome-project.svc.cluster.local
192.168.49.200:15443     HEALTHY     OK                outbound|5000||helloworld.my-awesome-project.svc.cluster.local
```