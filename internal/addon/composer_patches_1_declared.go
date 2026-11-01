package addon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// patchEntry is one patch as composer.json's extra.patches declares it.
type patchEntry struct {
	description string
	path        string
}

// declaredPatches is extra.patches in declaration order: composer-patches 1.x applies a package's
// patches in that order, so neither checking nor writing them back may reorder them.
type declaredPatches struct {
	packages []string
	sets     map[string][]patchEntry
}

// rename gives a patch a new description in place. A description another patch of the package
// already has is refused: JSON keys are unique, so one of the two would be lost.
func (d *declaredPatches) rename(pkg string, from string, to string) bool {
	set := d.sets[pkg]
	if slices.ContainsFunc(set, func(e patchEntry) bool { return e.description == to }) {
		return false
	}
	for i := range set {
		if set[i].description == from {
			set[i].description = to
			return true
		}
	}
	return false
}

func (d *declaredPatches) setPath(pkg string, description string, path string) {
	for i := range d.sets[pkg] {
		if d.sets[pkg][i].description == description {
			d.sets[pkg][i].path = path
		}
	}
}

// remove drops one patch, and the package with it once it has none left.
func (d *declaredPatches) remove(pkg string, description string) {
	d.sets[pkg] = slices.DeleteFunc(d.sets[pkg], func(e patchEntry) bool { return e.description == description })
	if len(d.sets[pkg]) == 0 {
		d.removePackage(pkg)
	}
}

func (d *declaredPatches) removePackage(pkg string) {
	delete(d.sets, pkg)
	d.packages = slices.DeleteFunc(d.packages, func(p string) bool { return p == pkg })
}

// UnmarshalJSON reads tokens rather than into a map, which would lose the declaration order.
func (d *declaredPatches) UnmarshalJSON(data []byte) error {
	*d = declaredPatches{sets: map[string][]patchEntry{}}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := expectDelim(dec, '{'); err != nil {
		return err
	}
	for dec.More() {
		pkg, err := stringToken(dec)
		if err != nil {
			return err
		}
		set, err := decodePatchSet(dec)
		if err != nil {
			return fmt.Errorf("patches of %s: %w", pkg, err)
		}
		if _, seen := d.sets[pkg]; !seen {
			d.packages = append(d.packages, pkg)
		}
		d.sets[pkg] = set
	}
	return expectDelim(dec, '}')
}

func decodePatchSet(dec *json.Decoder) ([]patchEntry, error) {
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	set := []patchEntry{}
	for dec.More() {
		description, err := stringToken(dec)
		if err != nil {
			return nil, err
		}
		var path string
		if err := dec.Decode(&path); err != nil {
			return nil, err
		}
		// A repeated key keeps its first position and its last value, as PHP's json_decode does.
		if i := slices.IndexFunc(set, func(e patchEntry) bool { return e.description == description }); i >= 0 {
			set[i].path = path
			continue
		}
		set = append(set, patchEntry{description: description, path: path})
	}
	return set, expectDelim(dec, '}')
}

func (d declaredPatches) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, pkg := range d.packages {
		if i > 0 {
			out.WriteByte(',')
		}
		writeJSONString(&out, pkg)
		out.WriteString(":{")
		for j, e := range d.sets[pkg] {
			if j > 0 {
				out.WriteByte(',')
			}
			writeJSONString(&out, e.description)
			out.WriteByte(':')
			writeJSONString(&out, e.path)
		}
		out.WriteByte('}')
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func writeJSONString(out *bytes.Buffer, s string) {
	encoded, _ := json.Marshal(s)
	out.Write(encoded)
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	if token != want {
		return fmt.Errorf("expected %q, found %v", want, token)
	}
	return nil
}

func stringToken(dec *json.Decoder) (string, error) {
	token, err := dec.Token()
	if err != nil {
		return "", err
	}
	s, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("expected a key, found %v", token)
	}
	return s, nil
}
