# Migrating to the namespaced localai API groups

The Agent, Model, Settings and Skill kinds were cluster-scoped and served from
the legacy `*.localai.crossplane.io/v1alpha1` groups. They are now namespaced
and served from `*.localai.m.crossplane.io/v1beta1`. The ProviderConfig group
moved to `localai.m.crossplane.io/v1beta1`.

Installing this package removes the old CRDs. Deleting a CRD deletes every
custom resource under it, and each resource's deletion policy then runs against
the LocalAI server. Migrate before upgrading.

## Steps

1. Set `spec.deletionPolicy: Orphan` on every existing managed resource, so
   removing the old objects leaves the LocalAI objects in place.
2. Record the existing resources (`kubectl get <kind> -o yaml`), then delete the
   old ones once they are orphaned.
3. Upgrade the provider package.
4. Recreate each resource under the new group with `metadata.namespace` set,
   using the same `forProvider` spec and the `*.localai.m.crossplane.io/v1beta1`
   apiVersion. Use the external-name annotation to adopt the existing LocalAI
   objects.
5. Update your ProviderConfig to `localai.m.crossplane.io/v1beta1`.
