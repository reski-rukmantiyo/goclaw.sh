package store

import (
	"context"
	"testing"
)

func TestIsSkillVisibleTo(t *testing.T) {
	alice := "alice"
	bob := "bob"
	ctx := WithUserID(context.Background(), alice)

	tests := []struct {
		name       string
		owner      string
		visibility string
		isSystem   bool
		want       bool
	}{
		{"system skill visible to anyone", "system", "private", true, true},
		{"public visible to non-owner", bob, "public", false, true},
		{"empty visibility treated as public", bob, "", false, true},
		{"private visible to owner", alice, "private", false, true},
		{"private hidden from non-owner", bob, "private", false, false},
		{"private with no owner treated as public", "", "private", false, true},
		{"unknown enum fails closed", bob, "team", false, false},
		{"uppercase private matched for owner", alice, "PRIVATE", false, true},
		{"whitespace public treated as public", bob, "  public  ", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSkillVisibleTo(ctx, tt.owner, tt.visibility, tt.isSystem)
			if got != tt.want {
				t.Fatalf("IsSkillVisibleTo(owner=%q, vis=%q, sys=%v) = %v, want %v",
					tt.owner, tt.visibility, tt.isSystem, got, tt.want)
			}
		})
	}
}

func TestFilterVisibleSkills(t *testing.T) {
	ctx := WithUserID(context.Background(), "alice")
	skills := []SkillInfo{
		{Slug: "sys", IsSystem: true, Visibility: "public"},
		{Slug: "mine-private", OwnerID: "alice", Visibility: "private"},
		{Slug: "theirs-private", OwnerID: "bob", Visibility: "private"},
		{Slug: "theirs-public", OwnerID: "bob", Visibility: "public"},
	}
	got := FilterVisibleSkills(ctx, skills)
	gotSlugs := map[string]bool{}
	for _, s := range got {
		gotSlugs[s.Slug] = true
	}
	for _, want := range []string{"sys", "mine-private", "theirs-public"} {
		if !gotSlugs[want] {
			t.Errorf("expected %q in filtered output, got %v", want, gotSlugs)
		}
	}
	if gotSlugs["theirs-private"] {
		t.Errorf("leaked private skill to non-owner: %v", gotSlugs)
	}
}

func TestCanTransitionScope(t *testing.T) {
	tests := []struct {
		role     string
		oldScope string
		newScope string
		want     bool
	}{
		// Promotions
		{"tenant_admin", ScopePersonal, ScopeGroup, true},
		{"group_admin", ScopePersonal, ScopeGroup, true},
		{"member", ScopePersonal, ScopeGroup, false},
		{"tenant_admin", ScopeGroup, ScopeTenant, true},
		{"group_admin", ScopeGroup, ScopeTenant, false},
		{"member", ScopeGroup, ScopeTenant, false},
		// Demotions
		{"tenant_admin", ScopeTenant, ScopeGroup, true},
		{"group_admin", ScopeTenant, ScopeGroup, false},
		{"tenant_admin", ScopeGroup, ScopePersonal, true},
		{"group_admin", ScopeGroup, ScopePersonal, true},
		// Same scope (no-op)
		{"member", ScopePersonal, ScopePersonal, true},
		{"member", ScopeGroup, ScopeGroup, true},
	}

	for _, tt := range tests {
		got := CanTransitionScope(tt.role, tt.oldScope, tt.newScope)
		if got != tt.want {
			t.Errorf("CanTransitionScope(%q, %q→%q) = %v, want %v",
				tt.role, tt.oldScope, tt.newScope, got, tt.want)
		}
	}
}

func TestIsResourceVisibleTo(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		scope    string
		ownerID  string
		groupID  string
		want     bool
	}{
		{
			name:    "personal visible to owner",
			ctx:     WithUserID(context.Background(), "alice"),
			scope:   ScopePersonal,
			ownerID: "alice",
			want:    true,
		},
		{
			name:    "personal hidden from non-owner",
			ctx:     WithUserID(context.Background(), "bob"),
			scope:   ScopePersonal,
			ownerID: "alice",
			want:    false,
		},
		{
			name:    "personal visible to admin",
			ctx:     WithUserID(WithRole(context.Background(), "admin"), "bob"),
			scope:   ScopePersonal,
			ownerID: "alice",
			want:    true,
		},
		{
			name:    "tenant visible to everyone",
			ctx:     WithUserID(context.Background(), "bob"),
			scope:   ScopeTenant,
			ownerID: "alice",
			want:    true,
		},
		{
			name:    "group visible with group role",
			ctx:     WithUserID(WithGroupRole(context.Background(), "member"), "bob"),
			scope:   ScopeGroup,
			ownerID: "alice",
			groupID: "group-123",
			want:    true,
		},
		{
			name:    "group hidden without group context",
			ctx:     WithUserID(context.Background(), "bob"),
			scope:   ScopeGroup,
			ownerID: "alice",
			groupID: "group-123",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsResourceVisibleTo(tt.ctx, tt.scope, tt.ownerID, tt.groupID)
			if got != tt.want {
				t.Errorf("IsResourceVisibleTo() = %v, want %v", got, tt.want)
			}
		})
	}
}
