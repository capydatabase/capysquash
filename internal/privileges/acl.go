// Package privileges carries a migration history's security posture into a
// squashed baseline: object ownership, the privileges granted and revoked on
// each object (tables, columns, sequences, routines, schemas, types), ALTER
// DEFAULT PRIVILEGES, roles and role membership.
//
// The consolidation pipeline reorders and merges object definitions, so a
// GRANT cannot simply stay next to the statement it followed. Instead the
// history is replayed through a model of PostgreSQL's access control lists:
// every GRANT, REVOKE, ownership change and default privilege is applied, in
// history order, to the objects that exist at that moment, following renames,
// SET SCHEMA and drops. The baseline then ends with a PRIVILEGES section that
// takes each object it creates from PostgreSQL's built-in default privileges
// to the net state the history leaves behind, after every object exists.
//
// Assumptions, stated where the output relies on them:
//
//   - One role runs the history (the migrating role) and the baseline, in one
//     session. The history can switch roles: SET ROLE, SET SESSION
//     AUTHORIZATION and SET LOCAL ROLE (inside BEGIN ... COMMIT) are
//     followed, so objects created as another role are that role's, ALTER
//     DEFAULT PRIVILEGES without FOR ROLE belongs to the current role, and
//     CURRENT_USER names it. The baseline creates everything as the migrating
//     role and writes the outcome out: ALTER ... OWNER TO for the owners, and
//     for privileges a role granted on an object it neither owns nor is a
//     member of the owner of (with a grant option it holds), SET ROLE around
//     the grant, since PostgreSQL records the grantor as the current role and
//     GRANTED BY cannot name another. The migrating role grants as the owner,
//     as PostgreSQL records it for superusers and owners.
//   - Objects start from PostgreSQL's built-in default privileges; default
//     privileges that already exist in the target database are not known, nor
//     are role memberships and superusers the history does not create.
//   - ALTER DEFAULT PRIVILEGES without FOR ROLE applies to the current role.
//     FOR ROLE naming a role only applies to history objects when that role
//     is the one running the migrations, which cannot be known statically: the
//     privileges that depend on it are emitted in a DO block that checks
//     current_user at apply time.
package privileges

import (
	"slices"
	"strings"
)

// Role tokens that cannot collide with a real role name.
const (
	rolePublic   = "\x00public"   // the PUBLIC pseudo-role
	roleMigrator = "\x00migrator" // the role running the migrations
)

// Privilege keywords as PostgreSQL spells them in GRANT.
const (
	privSelect     = "SELECT"
	privInsert     = "INSERT"
	privUpdate     = "UPDATE"
	privDelete     = "DELETE"
	privTruncate   = "TRUNCATE"
	privReferences = "REFERENCES"
	privTrigger    = "TRIGGER"
	privMaintain   = "MAINTAIN" // PostgreSQL 17+; only reachable through ALL before that
	privUsage      = "USAGE"
	privCreate     = "CREATE"
	privExecute    = "EXECUTE"
)

// aclClass is a family of objects that share a privilege universe and a
// built-in default: pg_class relations, sequences, columns, routines,
// schemas and types.
type aclClass int

const (
	classTable aclClass = iota
	classSequence
	classColumn
	classRoutine
	classSchema
	classType
)

// universe lists the privileges ALL expands to for a class, in the order
// PostgreSQL prints them.
func (c aclClass) universe() []string {
	switch c {
	case classTable:
		return []string{privSelect, privInsert, privUpdate, privDelete, privTruncate, privReferences, privTrigger, privMaintain}
	case classSequence:
		return []string{privUsage, privSelect, privUpdate}
	case classColumn:
		return []string{privSelect, privInsert, privUpdate, privReferences}
	case classRoutine:
		return []string{privExecute}
	case classSchema:
		return []string{privUsage, privCreate}
	case classType:
		return []string{privUsage}
	default:
		return nil
	}
}

// privSet maps each privilege held to whether it carries WITH GRANT OPTION.
type privSet map[string]bool

// acl maps each grantee (a role name, rolePublic or roleMigrator) to what it
// holds from one grantor: an object's acl holds what its owner granted, and
// object.delegated what other roles granted.
type acl map[string]privSet

func (a acl) clone() acl {
	out := make(acl, len(a))
	for grantee, privs := range a {
		copied := make(privSet, len(privs))
		for p, option := range privs {
			copied[p] = option
		}
		out[grantee] = copied
	}
	return out
}

// defaultACL is PostgreSQL's acldefault(): the owner holds every privilege
// (without a recorded grant option, which owners have implicitly), and PUBLIC
// may execute routines and use types. Columns have no default entries.
func defaultACL(class aclClass, owner string) acl {
	out := acl{}
	if class == classColumn {
		return out
	}
	owned := privSet{}
	for _, p := range class.universe() {
		owned[p] = false
	}
	out[owner] = owned
	switch class {
	case classRoutine:
		out[rolePublic] = privSet{privExecute: false}
	case classType:
		out[rolePublic] = privSet{privUsage: false}
	}
	return out
}

// grant adds privileges (and grant options) to grantee.
func (a acl) grant(grantee string, privs []string, withGrantOption bool) {
	held := a[grantee]
	if held == nil {
		held = privSet{}
		a[grantee] = held
	}
	for _, p := range privs {
		held[p] = held[p] || withGrantOption
	}
}

// revoke removes privileges from grantee, or only their grant options.
func (a acl) revoke(grantee string, privs []string, grantOptionOnly bool) {
	held := a[grantee]
	if held == nil {
		return
	}
	for _, p := range privs {
		if _, ok := held[p]; !ok {
			continue
		}
		if grantOptionOnly {
			held[p] = false
		} else {
			delete(held, p)
		}
	}
	if len(held) == 0 {
		delete(a, grantee)
	}
}

// changeOwner is PostgreSQL's aclnewowner(): the old owner's entry becomes
// the new owner's, merged with anything the new owner already held.
func (a acl) changeOwner(oldOwner, newOwner string) {
	if oldOwner == newOwner {
		return
	}
	held, ok := a[oldOwner]
	if !ok {
		return
	}
	delete(a, oldOwner)
	target := a[newOwner]
	if target == nil {
		a[newOwner] = held
		return
	}
	for p, option := range held {
		target[p] = target[p] || option
	}
}

// equal reports whether two ACLs grant the same privileges and options.
func (a acl) equal(b acl) bool {
	if len(a) != len(b) {
		return false
	}
	for grantee, privs := range a {
		other, ok := b[grantee]
		if !ok || len(other) != len(privs) {
			return false
		}
		for p, option := range privs {
			if o, ok := other[p]; !ok || o != option {
				return false
			}
		}
	}
	return true
}

// merge adds every entry of b to a (PostgreSQL's aclmerge).
func (a acl) merge(b acl) {
	for grantee, privs := range b {
		for p, option := range privs {
			a.grant(grantee, []string{p}, option)
		}
	}
}

// normalizePrivileges resolves a GRANT's privilege names against a class:
// nil or ALL means every privilege of the class, and privileges the class
// does not have are dropped (PostgreSQL warns and ignores them for a
// sequence named through ON TABLE).
func normalizePrivileges(class aclClass, names []string) []string {
	universe := class.universe()
	if len(names) == 0 {
		return universe
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		upper := strings.ToUpper(name)
		if upper == "ALL" || upper == "ALL PRIVILEGES" {
			return universe
		}
		if slices.Contains(universe, upper) && !slices.Contains(out, upper) {
			out = append(out, upper)
		}
	}
	return out
}
