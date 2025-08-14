load('ext://helm_resource', 'helm_resource', 'helm_repo')
load('ext://namespace', 'namespace_create')
load('ext://restart_process', 'docker_build_with_restart')

# Dockerfile for accurate-controller
ACCURATE_DOCKERFILE = '''FROM golang:alpine
WORKDIR /
COPY ./bin/accurate-controller /
CMD ["/accurate-controller"]
'''

def deploy_cert_manager():
    local('kubectl apply -f https://github.com/jetstack/cert-manager/releases/latest/download/cert-manager.yaml')
    # wait for the service to become available
    local("kubectl wait -n cert-manager --for=condition=Available --timeout=300s deployment cert-manager-webhook")

deploy_cert_manager()

# Generate manifest and go files
local_resource('make manifests', "make manifests", deps=["api", "controllers"], ignore=['*/*/*/zz_generated.deepcopy.go'])
local_resource('make generate', "make generate", deps=["api", "controllers"], ignore=['*/*/*/zz_generated.deepcopy.go'])

namespace_create('accurate')
accurate_manifests = helm(
  './charts/accurate',
  name='accurate',
  namespace='accurate',
  set=[
    'image.repository=accurate',
    'image.tag=dev',
    'image.pullPolicy=Never'
  ]
)
objects = decode_yaml_stream(accurate_manifests)

for o in objects:
    if o["kind"] == "Deployment" and o.get("metadata").get("name") in ["accurate-controller-manager"]:
        o["spec"]["template"]["spec"]["securityContext"] = {"runAsNonRoot": False, "runAsUser": 0, "readOnlyRootFilesystem": False}
        o["spec"]["template"]["spec"]["containers"][0]["securityContext"] = {"runAsNonRoot": False, "runAsUser": 0, "readOnlyRootFilesystem": False}
        o["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "Always"

overridden_acurate_manifests = encode_yaml_stream(objects)
k8s_yaml(overridden_acurate_manifests)

# build
operator_deps = ['api', 'controllers', 'cmd']
local_resource('Watch & Compile', 'make build', deps=operator_deps)

# Sample YAML
local_resource('Sample YAML', 'kubectl apply -f ./config/samples', deps=["./config/samples"])

# accurate-controller
docker_build_with_restart(
    'accurate:dev', '.',
    dockerfile_contents=ACCURATE_DOCKERFILE,
    entrypoint=['/accurate-controller'],
    only=['./bin/accurate-controller'],
    live_update=[
        sync('./bin/accurate-controller', '/accurate-controller'),
    ],
)
