
# ExternalDNS - GCore

Inspired from other 5 external DNS examples:
- https://github.com/stackitcloud/external-dns-stackit-webhook
- https://github.com/glesys/external-dns-glesys
- https://github.com/mconfalonieri/external-dns-hetzner-webhook
- https://github.com/bizflycloud/external-dns-bizflycloud-webhook
- https://github.com/mrueg/external-dns-netcup-webhook

Most of the codes taken from [this](//github.com/kubernetes-sigs/external-dns/pull/2203/commits) commits, 
with modification to match current [gcore SDK](//github.com/G-Core/gcore-dns-sdk-go).

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

        - image: ghcr.io/kokizzu/external-dns-gcore-webhook:v0.0.1
          name: gcore-webhook
          ports:
            - containerPort: 8888
          env:
            - name: GCORE_API_KEY
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
    external-dns.alpha.kubernetes.io/internal-hostname: nginxinternal.example.com.
    external-dns.alpha.kubernetes.io/hostname: nginx.example.com.

spec:
  selector:
    app: nginx
  type: LoadBalancer
  ports:
    - protocol: TCP
      port: 4080
      targetPort: 1080
```

## Local Deployment

```bash
export GCORE_PERMANENT_API_TOKEN=xxxxxxxxxxxxxxxxxxxxxx

CGO_ENABLED=0 go build
./external-dns-gcore

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