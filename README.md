
# ExternalDNS - Gcore

[![](https://img.shields.io/github/license/G-Core/external-dns-gcore-webhook?style=for-the-badge)](LICENSE)

ExternalDNS is a Kubernetes add-on for automatically managing Domain Name System (DNS) records for Kubernetes services by using different DNS providers. By default, Kubernetes manages DNS records internally, but ExternalDNS takes this functionality a step further by delegating the management of DNS records to an external DNS provider such as this one. Therefore, the Hetzner webhook allows to manage your Hetzner domains inside your kubernetes cluster with [ExternalDNS](//github.com/kubernetes-sigs/external-dns).

To use ExternalDNS with Gcore you need to get API token from https://accounts.gcore.com/profile/api-tokens.

## Deployment in kubernetes:

secret.yaml

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: external-dns-gcore-secret
stringData:
  GCORE_PERMANENT_API_TOKEN: "xxxxxxxxxxxxxxxxxxxxxx"
```

retrieve your own permanent API token from https://accounts.gcore.com/profile/api-tokens

`$ kubectl apply -f secret.yaml`

external-dns-gcore.yaml

```yaml
---

apiVersion: apps/v1
kind: Deployment
metadata:
  name: external-dns
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: external-dns
  template:
    metadata:
      labels:
        app: external-dns
    spec:
      serviceAccountName: external-dns
      containers:
        - name: external-dns
          image: registry.k8s.io/external-dns/external-dns:v0.14.0
          args:
            - --source=service
            - --source=ingress
            - --provider=webhook

        - image: ghcr.io/g-core/external-dns-gcore-webhook:v0.0.3
          name: gcore-webhook
          ports:
            - containerPort: 8888
          imagePullPolicy: Always
          env:
            - name: GCORE_PERMANENT_API_TOKEN
              valueFrom:
                secretKeyRef:
                  name: external-dns-gcore-secret
                  key: GCORE_PERMANENT_API_TOKEN

---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-dns
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: external-dns
rules:
  - apiGroups: [""]
    resources: ["services","endpoints","pods"]
    verbs: ["get","watch","list"]
  - apiGroups: ["extensions","networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get","watch","list"]
  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: external-dns-viewer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: external-dns
subjects:
  - kind: ServiceAccount
    name: external-dns
    namespace: default 
```

`$ kubectl apply -f external-dns-gcore.yaml`

to debug:

`$ kubectl logs -f external-dns-xxxxPod -c gcore-webhook`

Example deployment using external DNS:

```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
        - image: nginx
          name: nginx
          ports:
            - containerPort: 1080
---
apiVersion: v1
kind: Service
metadata:
  name: nginx
  annotations:
    external-dns.alpha.kubernetes.io/internal-hostname: example-internal.kokizzu.foo.bar.
    external-dns.alpha.kubernetes.io/hostname: example.kokizzu.foo.bar

spec:
  selector:
    app: nginx
  type: LoadBalancer
  ports:
    - protocol: TCP
      port: 4080
      targetPort: 1080
```

If doesn't work, make sure zone name is correct with one on gcore dns dashboard.

it would at least create something like this:

```
time="2024-02-07T12:03:52Z" level=debug msg="gcore: finishing get domain filters with [example.dev .example.dev kokizzu.foo.bar .kokizzu.foo.bar]"
time="2024-02-07T12:03:53Z" level=debug msg="gcore: finishing get records: 3"
time="2024-02-07T12:03:53Z" level=debug msg="returning records count: 3" requestMethod=GET requestPath=/records
time="2024-02-07T12:03:53Z" level=debug msg="requesting adjust endpoints count: 1"
time="2024-02-07T12:03:53Z" level=debug msg="return adjust endpoints response, resultEndpointCount: 1"
time="2024-02-07T12:03:53Z" level=debug msg="requesting apply changes, create: 3 , updateOld: 0, updateNew: 0, delete: 0" requestMethod=POST requestPath=/records
time="2024-02-07T12:03:53Z" level=info msg="gcore: starting apply changes createLen=3, deleteLen=0, updateOldLen=0, updateNewLen=0"
time="2024-02-07T12:03:53Z" level=debug msg="gcore: starting get domain filters"
time="2024-02-07T12:03:53Z" level=debug msg="gcore: finishing get domain filters with [example.dev .example.dev kokizzu.foo.bar .kokizzu.foo.bar]"
time="2024-02-07T12:03:53Z" level=debug msg="create example-internal.kokizzu.foo.bar A 10.111.253.251"
time="2024-02-07T12:03:53Z" level=debug msg="create example-internal.kokizzu.foo.bar TXT \"heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/nginx\""
time="2024-02-07T12:03:53Z" level=debug msg="create a-example-internal.kokizzu.foo.bar TXT \"heritage=external-dns,external-dns/owner=default,external-dns/resource=service/default/nginx\""
time="2024-02-07T12:03:54Z" level=info msg="gcore: finishing apply changes created=3, deleted=0, updated=0"
time="2024-02-07T12:04:51Z" level=debug msg="requesting records" requestMethod=GET requestPath=/records
```

![image](https://github.com/kokizzu/external-dns-gcore-webhook/assets/1061610/2be9dc0b-5971-468b-88dd-704e208eea2b)


## Local Deployment

```bash
export GCORE_PERMANENT_API_TOKEN=xxxxxxxxxxxxxxxxxxxxxx

CGO_ENABLED=0 go build
./external-dns-gcore-webhook

# In another terminal
curl http://localhost:8888/records -H 'Accept: application/external.dns.webhook+json;version=1'
# Example response:
[
  {
    "dnsName": "nginx.internal",
    "targets": [
      "10.99.88.77"
    ],
    "recordType": "A",
    "recordTTL": 3600
  }
]
```

## How To Contribute

Any pull request are welcome, please make sure to include the test case.

Inspired from other 5 external DNS examples if you need further reference:
- https://github.com/stackitcloud/external-dns-stackit-webhook
- https://github.com/glesys/external-dns-glesys
- https://github.com/mconfalonieri/external-dns-hetzner-webhook
- https://github.com/bizflycloud/external-dns-bizflycloud-webhook
- https://github.com/mrueg/external-dns-netcup-webhook

