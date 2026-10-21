//go:build generate

/*
Copyright 2025 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// NOTE: This provider uses a native HTTP client and does not use angryjet.
// Interface method implementations in zz_generated.managed.go are
// written by hand and must not be overwritten by code generators.

// Remove existing CRDs
//go:generate rm -rf ../package/crds

// Generate CRD manifests only. Deepcopy (zz_generated.deepcopy.go) and
// managed interface methods (zz_generated.managed.go) are maintained by hand
// because controller-gen v0.21 does not correctly handle embedded crossplane
// types from crossplane/crossplane/apis/v2.
// Note: We specify explicit paths to avoid controller-gen trying to generate
// CRDs for ProviderConfigUsage in each API group package.
//go:generate go run -tags generate sigs.k8s.io/controller-tools/cmd/controller-gen crd:crdVersions=v1 paths=./v1beta1 paths=./authenticationflow/v1beta1 paths=./authorizationpolicy/v1beta1 paths=./authz/v1beta1 paths=./client/v1beta1 paths=./clientcertificates/v1beta1 paths=./clientinitialaccess/v1beta1 paths=./component/v1beta1 paths=./events/v1beta1 paths=./group/v1beta1 paths=./identityprovider/v1beta1 paths=./keys/v1beta1 paths=./openidclient/v1beta1 paths=./realm/v1beta1 paths=./realmimpexp/v1beta1 paths=./role/v1beta1 paths=./rolemappings/v1beta1 paths=./scopes/v1beta1 paths=./user/v1beta1 paths=./userfederation/v1beta1 output:artifacts:config=../package/crds

package apis
