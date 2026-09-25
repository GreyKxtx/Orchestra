// Package contract owns the Contract Epoch layer (spec §5.3–5.4):
// .orchestra/contract/EPOCH.yaml with versions, sha256 and owners of the
// frozen design artifacts, hash verification of WorkOrder contract_refs and
// the built-in Artifact Verify checks. The runtime maintains EPOCH.yaml —
// models never write it.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/orchestra/orchestra/internal/wsview"
	"github.com/orchestra/orchestra/patch/fsutil"
)

// DirRel is the contract artifacts directory relative to the project root.
const DirRel = ".orchestra/contract"

// EpochFileRel is the epoch file path relative to the project root.
const EpochFileRel = ".orchestra/contract/EPOCH.yaml"

// Artifact is one frozen contract artifact entry in EPOCH.yaml.
type Artifact struct {
	Version int    `yaml:"version"`
	SHA256  string `yaml:"sha256"`
	Owner   string `yaml:"owner,omitempty"`
}

// Epoch mirrors EPOCH.yaml: a monotonic epoch counter plus per-artifact
// versions and hashes, keyed by file name inside .orchestra/contract/.
type Epoch struct {
	Epoch     int                 `yaml:"epoch"`
	Artifacts map[string]Artifact `yaml:"artifacts"`
}

// Ref is a WorkOrder contract reference: the artifact the Lead read when
// producing the WorkOrder, with the hash of that version.
type Ref struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Load reads EPOCH.yaml. A missing file is not an error: (nil, false, nil) —
// the contract layer is simply not adopted in this project yet.
func Load(projectRoot string) (*Epoch, bool, error) {
	data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(EpochFileRel)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", EpochFileRel, err)
	}
	var e Epoch
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", EpochFileRel, err)
	}
	return &e, true, nil
}

// Save writes EPOCH.yaml atomically.
func Save(projectRoot string, e *Epoch) error {
	if e == nil {
		return fmt.Errorf("nil epoch")
	}
	data, err := yaml.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal epoch: %w", err)
	}
	path := filepath.Join(projectRoot, filepath.FromSlash(EpochFileRel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.AtomicWriteFile(path, data, 0o644)
}

func contentSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// artifactSHA256 hashes .orchestra/contract/{name} as v sees it.
func artifactSHA256(v wsview.View, name string) (string, error) {
	data, err := v.ReadFile(DirRel + "/" + name)
	if err != nil {
		return "", err
	}
	return contentSHA256(data), nil
}

// UpdateArtifact re-hashes .orchestra/contract/{name} as v sees it, bumps its
// version and the global epoch, and persists EPOCH.yaml. This is the runtime
// side of an accepted contract_change_request (spec §5.3): only the artifact
// owner may trigger it; ownership arbitration happens above this call.
//
// The artifact is read through v because agents stage their writes: the
// version a turn works with is the staged one, and the disk has it only once
// the turn applies.
func UpdateArtifact(projectRoot string, v wsview.View, name, owner string) (*Epoch, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) {
		return nil, fmt.Errorf("contract: artifact name must be a bare file name inside %s, got %q", DirRel, name)
	}
	sha, err := artifactSHA256(v, name)
	if err != nil {
		return nil, fmt.Errorf("contract: hash %s: %w", name, err)
	}
	e, found, err := Load(projectRoot)
	if err != nil {
		return nil, err
	}
	if !found {
		e = &Epoch{}
	}
	if e.Artifacts == nil {
		e.Artifacts = map[string]Artifact{}
	}
	art := e.Artifacts[name]
	if art.SHA256 == sha {
		// No content change — idempotent, no epoch bump.
		if art.Owner == "" && owner != "" {
			art.Owner = owner
			e.Artifacts[name] = art
			if err := Save(projectRoot, e); err != nil {
				return nil, err
			}
		}
		return e, nil
	}
	art.Version++
	art.SHA256 = sha
	if owner != "" {
		art.Owner = owner
	}
	e.Artifacts[name] = art
	e.Epoch++
	if err := Save(projectRoot, e); err != nil {
		return nil, err
	}
	return e, nil
}

// artifactName maps a ref path (".orchestra/contract/Domain_Model.md" or a
// bare "Domain_Model.md") to the EPOCH.yaml key.
func artifactName(refPath string) string {
	p := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(refPath)), "./")
	p = strings.TrimPrefix(p, DirRel+"/")
	return p
}

// VerifyRefs checks WorkOrder contract_refs (spec §5.3). A ref must name an
// artifact frozen in EPOCH.yaml and carry the hash of the version the task
// sees through v. Anything else fails with blocked_reason=stale_contract
// semantics: the WorkOrder was produced against another version of the
// contract and must be regenerated by its Lead.
//
// The hash is checked against the artifact itself, not only against the one
// EPOCH.yaml recorded. The epoch file is written once and lives on; the
// artifacts are staged with the turn that wrote them. When they part — a
// dry-run turn froze staged artifacts and ended, or an owner's change has not
// reached the epoch yet — a ref copied from EPOCH.yaml would pass while the
// task builds against a contract that is not there. What a task sees is what
// it builds against, so that is what its WorkOrder must describe.
func VerifyRefs(projectRoot string, v wsview.View, refs []Ref) error {
	if len(refs) == 0 {
		return nil
	}
	e, found, err := Load(projectRoot)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("stale_contract: WorkOrder carries contract_refs but %s does not exist; unblock: freeze the contract (stage 2.5) or drop the refs", EpochFileRel)
	}
	for _, r := range refs {
		name := artifactName(r.Path)
		if name == "" || strings.Contains(name, "/") {
			return fmt.Errorf("stale_contract: contract_ref path %q is not an artifact inside %s", r.Path, DirRel)
		}
		if _, ok := e.Artifacts[name]; !ok {
			return fmt.Errorf("stale_contract: artifact %q is not registered in %s; unblock: owner runs contract update for it", name, EpochFileRel)
		}
		want := strings.ToLower(strings.TrimSpace(r.SHA256))
		if want == "" {
			return fmt.Errorf("stale_contract: contract_ref for %q has empty sha256", name)
		}
		current, err := artifactSHA256(v, name)
		if err != nil {
			return fmt.Errorf("stale_contract: %q is frozen in epoch %d but not readable (%v); unblock: its owner restores it, then contract_freeze", name, e.Epoch, err)
		}
		if want != current {
			return fmt.Errorf("stale_contract: %q hash mismatch (WorkOrder %s… vs current %s…, epoch %d); unblock: Lead regenerates the WorkOrder against the current contract",
				name, shortHash(want), shortHash(current), e.Epoch)
		}
	}
	return nil
}

// Refresh records in EPOCH.yaml the artifacts among changed (project-relative
// paths) whose content v now holds, and returns the names whose version
// moved. Nothing happens before the contract is adopted: the first freeze is
// contract_freeze's, never a side effect of a write.
func Refresh(projectRoot string, v wsview.View, changed []string) ([]string, error) {
	var names []string
	for _, p := range changed {
		if name, ok := ArtifactFileName(p); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	before, found, err := Load(projectRoot)
	if err != nil || !found {
		return nil, err
	}
	var moved []string
	for _, name := range names {
		e, err := UpdateArtifact(projectRoot, v, name, "")
		if err != nil {
			return moved, err
		}
		if e.Artifacts[name].Version != before.Artifacts[name].Version {
			moved = append(moved, name)
		}
	}
	return moved, nil
}

// ArtifactFileName reports whether rel is a contract artifact — a file
// directly inside .orchestra/contract/ other than EPOCH.yaml — and returns
// its bare name.
func ArtifactFileName(rel string) (string, bool) {
	p := wsview.Clean(rel)
	rest, ok := strings.CutPrefix(p, DirRel+"/")
	if !ok || rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	if rest == filepath.Base(EpochFileRel) {
		return "", false
	}
	return rest, true
}

func shortHash(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
