package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewGroupService_UsesDefaultFailoverValues(t *testing.T) {
	svc := NewGroupService(nil)

	defaults := svc.GetFailoverDefaults()
	assert.Equal(t, 2, defaults.MaxSwitches)
	assert.Equal(t, 1, defaults.SameModelRetries)
	assert.Equal(t, 60, defaults.CooldownSeconds)
}

func TestSetFailoverDefaults_UpdatesAllThree(t *testing.T) {
	svc := NewGroupService(nil)

	svc.SetFailoverDefaults(5, 3, 120)

	defaults := svc.GetFailoverDefaults()
	assert.Equal(t, 5, defaults.MaxSwitches)
	assert.Equal(t, 3, defaults.SameModelRetries)
	assert.Equal(t, 120, defaults.CooldownSeconds)
}

func TestSetFailoverDefaults_PartialUpdate(t *testing.T) {
	svc := NewGroupService(nil)

	svc.SetFailoverDefaults(4, -1, -1)

	defaults := svc.GetFailoverDefaults()
	assert.Equal(t, 4, defaults.MaxSwitches)
	assert.Equal(t, 1, defaults.SameModelRetries)
	assert.Equal(t, 60, defaults.CooldownSeconds)
}

func TestSetFailoverDefaults_ZeroSameModelRetriesIsValid(t *testing.T) {
	svc := NewGroupService(nil)

	svc.SetFailoverDefaults(1, 0, 120)

	defaults := svc.GetFailoverDefaults()
	assert.Equal(t, 1, defaults.MaxSwitches)
	assert.Equal(t, 0, defaults.SameModelRetries)
	assert.Equal(t, 120, defaults.CooldownSeconds)
}

func TestSetFailoverDefaults_NegativeValuesIgnored(t *testing.T) {
	svc := NewGroupService(nil)

	svc.SetFailoverDefaults(-1, -1, -1)

	defaults := svc.GetFailoverDefaults()
	assert.Equal(t, 2, defaults.MaxSwitches)
	assert.Equal(t, 1, defaults.SameModelRetries)
	assert.Equal(t, 60, defaults.CooldownSeconds)
}

func TestGetFailoverDefaults_ReturnsCopy(t *testing.T) {
	svc := NewGroupService(nil)

	defaults := svc.GetFailoverDefaults()
	defaults.MaxSwitches = 99

	defaults2 := svc.GetFailoverDefaults()
	assert.Equal(t, 2, defaults2.MaxSwitches, "modifying the returned struct should not affect the service")
}

func TestFailoverDefaults_MultipleChainedSet(t *testing.T) {
	svc := NewGroupService(nil)

	svc.SetFailoverDefaults(3, 2, 90)
	svc.SetFailoverDefaults(4, 1, 150)

	defaults := svc.GetFailoverDefaults()
	assert.Equal(t, 4, defaults.MaxSwitches)
	assert.Equal(t, 1, defaults.SameModelRetries)
	assert.Equal(t, 150, defaults.CooldownSeconds)
}