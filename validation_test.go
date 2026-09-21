package library_test

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/stretchr/testify/require"
)

func TestInvalidReplacementPreservesPriorDNSAndState(t *testing.T) {
	fake := newFakeNamecheapAPI(t)
	executor := consumerExecutor(t, t.TempDir(), consumerInputs(
		"prior.com", "A", "192.0.2.1", fake.URL(), "key",
	))
	prior := applySavedPlan(t, executor, savedPlan(t, executor))
	require.Equal(t, []fakeAPIRecord{{
		name: "www", typeName: "A", address: "192.0.2.1",
	}}, fake.domainRecords("prior.com"))

	executor.Inputs = consumerInputs(
		"invalid.com", "CAA", `0 issue "example.com`, fake.URL(), "key",
	)
	plan := savedPlan(t, executor)
	resourceStep(t, plan, runtime.DecisionReplace)
	mutations := fake.mutationCount()
	_, err := executor.ApplyPlan(t.Context(), plan)
	require.ErrorContains(t, err, "mismatched quotes")
	require.Equal(t, mutations, fake.mutationCount())
	require.Equal(t, []fakeAPIRecord{{
		name: "www", typeName: "A", address: "192.0.2.1",
	}}, fake.domainRecords("prior.com"))
	require.Empty(t, fake.domainRecords("invalid.com"))
	requirePriorEntry(t, executor, prior)
}

func TestCredentialRotationUsesCurrentCredentialsWithoutReplacement(t *testing.T) {
	fake := newFakeNamecheapAPI(t)
	executor := consumerExecutor(t, t.TempDir(), consumerInputs(
		"example.com", "A", "192.0.2.1", fake.URL(), "old-key",
	))
	applySavedPlan(t, executor, savedPlan(t, executor))
	mutations := fake.mutationCount()

	fake.setAllowedKey("current-key")
	executor.Inputs = consumerInputs(
		"example.com", "A", "192.0.2.1", fake.URL(), "current-key",
	)
	plan := savedPlan(t, executor)
	resourceStep(t, plan, runtime.DecisionNoOp)
	applySavedPlan(t, executor, plan)
	require.Equal(t, mutations, fake.mutationCount())
}

func TestTargetChangeReplacesUsingCurrentCredentials(t *testing.T) {
	priorAPI := newFakeNamecheapAPI(t)
	desiredAPI := newFakeNamecheapAPI(t)
	executor := consumerExecutor(t, t.TempDir(), consumerInputs(
		"example.com", "A", "192.0.2.1", priorAPI.URL(), "old-key",
	))
	applySavedPlan(t, executor, savedPlan(t, executor))

	priorAPI.setAllowedKey("current-key")
	desiredAPI.setAllowedKey("current-key")
	executor.Inputs = consumerInputs(
		"example.com", "A", "192.0.2.1", desiredAPI.URL(), "current-key",
	)
	plan := savedPlan(t, executor)
	step := resourceStep(t, plan, runtime.DecisionReplace)
	require.Equal(t, []string{"base-url"}, step.ReplacementReasons)
	applySavedPlan(t, executor, plan)

	require.Empty(t, priorAPI.domainRecords("example.com"))
	require.Equal(t, []fakeAPIRecord{{
		name: "www", typeName: "A", address: "192.0.2.1",
	}}, desiredAPI.domainRecords("example.com"))
}

func TestInaccessibleReplacementTargetPreservesPriorDNSAndState(t *testing.T) {
	priorAPI := newFakeNamecheapAPI(t)
	desiredAPI := newFakeNamecheapAPI(t)
	executor := consumerExecutor(t, t.TempDir(), consumerInputs(
		"example.com", "A", "192.0.2.1", priorAPI.URL(), "old-key",
	))
	prior := applySavedPlan(t, executor, savedPlan(t, executor))
	mutations := priorAPI.mutationCount()

	priorAPI.setAllowedKey("current-key")
	desiredAPI.setAllowedKey("different-key")
	executor.Inputs = consumerInputs(
		"example.com", "A", "192.0.2.1", desiredAPI.URL(), "current-key",
	)
	plan := savedPlan(t, executor)
	resourceStep(t, plan, runtime.DecisionReplace)
	_, err := executor.ApplyPlan(t.Context(), plan)
	require.ErrorContains(t, err, "validate domain access")

	require.Equal(t, mutations, priorAPI.mutationCount())
	require.Equal(t, []fakeAPIRecord{{
		name: "www", typeName: "A", address: "192.0.2.1",
	}}, priorAPI.domainRecords("example.com"))
	require.Empty(t, desiredAPI.domainRecords("example.com"))
	requirePriorEntry(t, executor, prior)
}
