package adapter

import "testing"

type providerIdentityAliasA struct {
	NodeModulesAdapter
}

type providerIdentityAliasB struct {
	NodeModulesAdapter
}

func TestProviderIdentityTracksConcreteMembershipAsSortedMultiset(t *testing.T) {
	nodeModules := &NodeModulesAdapter{}
	buildCache := &BuildCacheAdapter{}
	pipCache := &PipCacheAdapter{}

	baseline := providerIdentity([]DebrisProvider{nodeModules, buildCache})

	if reordered := providerIdentity([]DebrisProvider{buildCache, nodeModules}); reordered != baseline {
		t.Errorf("reordered identity = %q; want %q", reordered, baseline)
	}

	if added := providerIdentity([]DebrisProvider{nodeModules, buildCache, pipCache}); added == baseline {
		t.Errorf("added-provider identity = %q; want different from %q", added, baseline)
	}

	if removed := providerIdentity([]DebrisProvider{nodeModules}); removed == baseline {
		t.Errorf("removed-provider identity = %q; want different from %q", removed, baseline)
	}

	if duplicated := providerIdentity([]DebrisProvider{nodeModules, buildCache, nodeModules}); duplicated == baseline {
		t.Errorf("duplicate-provider identity = %q; want different from %q", duplicated, baseline)
	}
}

func TestProviderIdentityUsesConcreteTypesInsteadOfProviderLabels(t *testing.T) {
	first := &providerIdentityAliasA{}
	second := &providerIdentityAliasB{}
	if first.Name() != second.Name() || first.Category() != second.Category() {
		t.Fatal("fixture providers must expose identical tool and category labels")
	}

	firstIdentity := providerIdentity([]DebrisProvider{first})
	secondIdentity := providerIdentity([]DebrisProvider{second})
	if firstIdentity == secondIdentity {
		t.Errorf("identities = %q; want distinct identities for different concrete types", firstIdentity)
	}
}
