package typesystem

import (
	"fmt"
	"path"

	"github.com/ipld/go-ipld-prime"
)

func Validate(ts Universe, t Type, node ipld.Node) []error {
	return validate(ts, t, node, "/")
}

// review: 'ts' param might not actually be necessary; everything relevant can be reached from t so far.
func validate(ts Universe, t Type, node ipld.Node, pth string) []error {
	switch t2 := t.(type) {
	case TypeBool:
		if node.ReprKind() != ipld.ReprKind_Bool {
			return []error{fmt.Errorf("Schema match failed: expected type %q (which is kind %v) at path %q, but found kind %v", t2.Name(), t.ReprKind(), pth, node.ReprKind())}
		}
		return nil
	case TypeString:
		if node.ReprKind() != ipld.ReprKind_String {
			return []error{fmt.Errorf("Schema match failed: expected type %q (which is kind %v) at path %q, but found kind %v", t2.Name(), t.ReprKind(), pth, node.ReprKind())}
		}
		return nil
	case TypeBytes:
		if node.ReprKind() != ipld.ReprKind_Bytes {
			return []error{fmt.Errorf("Schema match failed: expected type %q (which is kind %v) at path %q, but found kind %v", t2.Name(), t.ReprKind(), pth, node.ReprKind())}
		}
		return nil
	case TypeInt:
		if node.ReprKind() != ipld.ReprKind_Int {
			return []error{fmt.Errorf("Schema match failed: expected type %q (which is kind %v) at path %q, but found kind %v", t2.Name(), t.ReprKind(), pth, node.ReprKind())}
		}
		return nil
	case TypeFloat:
		if node.ReprKind() != ipld.ReprKind_Float {
			return []error{fmt.Errorf("Schema match failed: expected type %q (which is kind %v) at path %q, but found kind %v", t2.Name(), t.ReprKind(), pth, node.ReprKind())}
		}
		return nil
	case TypeMap:
		if node.ReprKind() != ipld.ReprKind_Map {
			return []error{fmt.Errorf("Schema match failed: expected type %q (which is kind %v) at path %q, but found kind %v", t2.Name(), t.ReprKind(), pth, node.ReprKind())}
		}
		keys, _ := node.KeysImmediate()
		errs := []error(nil)
		for _, k := range keys {
			// FUTURE: if KeyType is an enum rather than string, do membership check.
			child, _ := node.TraverseField(k)
			if child.IsNull() {
				if !t2.ValueIsNullable() {
					errs = append(errs, fmt.Errorf("Schema match failed: map at path %q contains unpermitted null in key %q", pth, k))
				}
			} else {
				errs = append(errs, validate(ts, t2.ValueType(), child, path.Join(pth, k))...)
			}
		}
		return errs
	case TypeList:
	case TypeLink:
		// TODO interesting case: would need resolver to keep checking.
	case TypeUnion:
		// TODO *several* interesting errors
	case TypeStruct:
		switch t2.tupleStyle {
		case false: // as map!
			if node.ReprKind() != ipld.ReprKind_Map {
				return []error{fmt.Errorf("Schema match failed: expected type %q (which is kind %v) at path %q, but found kind %v", t2.Name(), t.ReprKind(), pth, node.ReprKind())}
			}
			// TODO loop over em
			// TODO REVIEW order strictness questions?
		case true: // as array!

		}
	case TypeEnum:
		// TODO another interesting error
	}
	return nil
}
