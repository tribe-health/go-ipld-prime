package bindnode

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipld/go-ipld-prime/node/basicnode"
	"github.com/ipld/go-ipld-prime/schema"
)

func reprNode(node datamodel.Node) datamodel.Node {
	if node, ok := node.(schema.TypedNode); ok {
		return node.Representation()
	}
	// datamodel.Absent and datamodel.Null are not typed.
	// TODO: is this a problem? surely a typed struct's fields are always
	// typed, even when absent or null.
	return node
}

func reprStrategy(typ schema.Type) interface{} {
	switch typ := typ.(type) {
	case *schema.TypeStruct:
		return typ.RepresentationStrategy()
	case *schema.TypeUnion:
		return typ.RepresentationStrategy()
	}
	return nil
}

type _prototypeRepr _prototype

func (w *_prototypeRepr) NewBuilder() datamodel.NodeBuilder {
	return &_builderRepr{_assemblerRepr{
		schemaType: w.schemaType,
		val:        reflect.New(w.goType).Elem(),
	}}
}

type _nodeRepr _node

func (w *_nodeRepr) Kind() datamodel.Kind {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Stringjoin:
		return datamodel.Kind_String
	case schema.StructRepresentation_Map:
		return datamodel.Kind_Map
	case schema.StructRepresentation_Tuple:
		return datamodel.Kind_List
	case schema.UnionRepresentation_Keyed:
		return datamodel.Kind_Map
	case schema.UnionRepresentation_Kinded:
		haveIdx, _ := unionMember(w.val)
		mtyp := w.schemaType.(*schema.TypeUnion).Members()[haveIdx] // TODO this is fragile: panics with index-out-of-range if the user has nil'd all fields.
		return mtyp.RepresentationBehavior()
	case schema.UnionRepresentation_Stringprefix:
		return datamodel.Kind_String
	case nil:
		return (*_node)(w).Kind()
	default:
		panic(fmt.Sprintf("TODO Kind: %T", stg))
	}
}

func outboundMappedKey(stg schema.StructRepresentation_Map, key string) string {
	// TODO: why doesn't stg just allow us to "get" by the key string?
	field := schema.SpawnStructField(key, "", false, false)
	mappedKey := stg.GetFieldKey(field)
	return mappedKey
}

func inboundMappedKey(typ *schema.TypeStruct, stg schema.StructRepresentation_Map, key string) string {
	// TODO: can't do a "reverse" lookup... needs better API probably.
	fields := typ.Fields()
	for _, field := range fields {
		mappedKey := stg.GetFieldKey(field)
		if key == mappedKey {
			// println(key, "rev-mapped to", field.Name())
			return field.Name()
		}
	}
	// println(key, "had no mapping")
	return key // fallback to the same key
}

func outboundMappedType(stg schema.UnionRepresentation_Keyed, key string) string {
	// TODO: why doesn't stg just allow us to "get" by the key string?
	typ := schema.SpawnBool(key)
	mappedKey := stg.GetDiscriminant(typ)
	return mappedKey
}

func inboundMappedType(typ *schema.TypeUnion, stg schema.UnionRepresentation_Keyed, key string) string {
	// TODO: can't do a "reverse" lookup... needs better API probably.
	for _, member := range typ.Members() {
		mappedKey := stg.GetDiscriminant(member)
		if key == mappedKey {
			// println(key, "rev-mapped to", field.Name())
			return member.Name()
		}
	}
	// println(key, "had no mapping")
	return key // fallback to the same key
}

func (w *_nodeRepr) asKinded(stg schema.UnionRepresentation_Kinded, kind datamodel.Kind) *_nodeRepr {
	name := stg.GetMember(kind)
	members := w.schemaType.(*schema.TypeUnion).Members()
	for i, member := range members {
		if member.Name() != name {
			continue
		}
		w2 := *w
		w2.val = w.val.Field(i).Elem()
		w2.schemaType = member
		return &w2
	}
	panic("TODO: GetMember result is missing?")
}

func (w *_nodeRepr) LookupByString(key string) (datamodel.Node, error) {
	if stg, ok := reprStrategy(w.schemaType).(schema.UnionRepresentation_Kinded); ok {
		w = w.asKinded(stg, datamodel.Kind_Map)
	}
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Map:
		revKey := inboundMappedKey(w.schemaType.(*schema.TypeStruct), stg, key)
		v, err := (*_node)(w).LookupByString(revKey)
		if err != nil {
			return nil, err
		}
		return reprNode(v), nil
	case schema.UnionRepresentation_Keyed:
		revKey := inboundMappedType(w.schemaType.(*schema.TypeUnion), stg, key)
		v, err := (*_node)(w).LookupByString(revKey)
		if err != nil {
			return nil, err
		}
		return reprNode(v), nil
	case nil:
		v, err := (*_node)(w).LookupByString(key)
		if err != nil {
			return nil, err
		}
		return reprNode(v), nil
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_nodeRepr) LookupByIndex(idx int64) (datamodel.Node, error) {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Tuple:
		fields := w.schemaType.(*schema.TypeStruct).Fields()
		field := fields[idx]
		v, err := (*_node)(w).LookupByString(field.Name())
		if err != nil {
			return nil, err
		}
		return reprNode(v), nil
	case nil:
		v, err := (*_node)(w).LookupByIndex(idx)
		if err != nil {
			return nil, err
		}
		return reprNode(v), nil
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_nodeRepr) LookupBySegment(seg datamodel.PathSegment) (datamodel.Node, error) {
	switch w.Kind() {
	case datamodel.Kind_Map:
		return w.LookupByString(seg.String())
	case datamodel.Kind_List:
		idx, err := seg.Index()
		if err != nil {
			return nil, err
		}
		return w.LookupByIndex(idx)
	}
	return nil, datamodel.ErrWrongKind{
		TypeName:        w.schemaType.Name(),
		MethodName:      "LookupBySegment",
		AppropriateKind: datamodel.KindSet_Recursive,
		ActualKind:      w.Kind(),
	}
}

func (w *_nodeRepr) LookupByNode(key datamodel.Node) (datamodel.Node, error) {
	switch w.Kind() {
	case datamodel.Kind_Map:
		s, err := key.AsString()
		if err != nil {
			return nil, err
		}
		return w.LookupByString(s)
	case datamodel.Kind_List:
		i, err := key.AsInt()
		if err != nil {
			return nil, err
		}
		return w.LookupByIndex(i)
	}
	return nil, datamodel.ErrWrongKind{
		TypeName:        w.schemaType.Name(),
		MethodName:      "LookupByNode",
		AppropriateKind: datamodel.KindSet_Recursive,
		ActualKind:      w.Kind(),
	}
}

func (w *_nodeRepr) MapIterator() datamodel.MapIterator {
	if stg, ok := reprStrategy(w.schemaType).(schema.UnionRepresentation_Kinded); ok {
		w = w.asKinded(stg, datamodel.Kind_Map)
	}
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Map:
		itr := (*_node)(w).MapIterator().(*_structIterator)
		itr.reprEnd = int(w.lengthMinusTrailingAbsents())
		return (*_structIteratorRepr)(itr)
	case schema.UnionRepresentation_Keyed:
		itr := (*_node)(w).MapIterator().(*_unionIterator)
		return (*_unionIteratorRepr)(itr)
	case nil:
		switch st := (w.schemaType).(type) {
		case *schema.TypeMap:
			return &_mapIteratorRepr{
				schemaType: st,
				keysVal:    w.val.FieldByName("Keys"),
				valuesVal:  w.val.FieldByName("Values"),
			}
		default:
			panic(fmt.Sprintf("TODO: mapitr.repr for typekind %s", w.schemaType.TypeKind()))
		}
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

type _mapIteratorRepr _mapIterator

func (w *_mapIteratorRepr) Next() (key, value datamodel.Node, _ error) {
	k, v, err := (*_mapIterator)(w).Next()
	if err != nil {
		return nil, nil, err
	}
	return k.(schema.TypedNode).Representation(), v.(schema.TypedNode).Representation(), nil
}

func (w *_mapIteratorRepr) Done() bool {
	return w.nextIndex >= w.keysVal.Len()
}

func (w *_nodeRepr) ListIterator() datamodel.ListIterator {
	if reprStrategy(w.schemaType) == nil {
		return (*_listIteratorRepr)((*_node)(w).ListIterator().(*_listIterator))
	}
	// TODO this is probably failing silently for structs with tuple representation.
	return nil
}

type _listIteratorRepr _listIterator

func (w *_listIteratorRepr) Next() (index int64, value datamodel.Node, _ error) {
	idx, v, err := (*_listIterator)(w).Next()
	if err != nil {
		return idx, nil, err
	}
	return idx, v.(schema.TypedNode).Representation(), nil
}

func (w *_listIteratorRepr) Done() bool {
	return w.nextIndex >= w.val.Len()
}

func (w *_nodeRepr) lengthMinusTrailingAbsents() int64 {
	fields := w.schemaType.(*schema.TypeStruct).Fields()
	for i := len(fields) - 1; i >= 0; i-- {
		field := fields[i]
		if !field.IsOptional() || !w.val.Field(i).IsNil() {
			return int64(i + 1)
		}
	}
	return 0
}

func (w *_nodeRepr) Length() int64 {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Stringjoin:
		return -1
	case schema.StructRepresentation_Map:
		return w.lengthMinusTrailingAbsents()
	case schema.StructRepresentation_Tuple:
		return w.lengthMinusTrailingAbsents()
	case schema.UnionRepresentation_Keyed:
		return (*_node)(w).Length()
	case schema.UnionRepresentation_Kinded:
		w = w.asKinded(stg, w.Kind())
		// continues below
	case nil:
		// continues below
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
	return (*_node)(w).Length()
}

func (w *_nodeRepr) IsAbsent() bool {
	if reprStrategy(w.schemaType) == nil {
		return (*_node)(w).IsAbsent()
	}
	return false
}

func (w *_nodeRepr) IsNull() bool {
	if reprStrategy(w.schemaType) == nil {
		return (*_node)(w).IsNull()
	}
	return false
}

func (w *_nodeRepr) AsBool() (bool, error) {
	if reprStrategy(w.schemaType) == nil {
		return (*_node)(w).AsBool()
	}
	return false, datamodel.ErrWrongKind{
		TypeName:        w.schemaType.Name(),
		MethodName:      "AsBool",
		AppropriateKind: datamodel.KindSet_JustBool,
		ActualKind:      w.Kind(),
	}
}

func (w *_nodeRepr) AsInt() (int64, error) {
	if reprStrategy(w.schemaType) == nil {
		return (*_node)(w).AsInt()
	}
	return 0, datamodel.ErrWrongKind{
		TypeName:        w.schemaType.Name(),
		MethodName:      "AsInt",
		AppropriateKind: datamodel.KindSet_JustInt,
		ActualKind:      w.Kind(),
	}
}

func (w *_nodeRepr) AsFloat() (float64, error) {
	if reprStrategy(w.schemaType) == nil {
		return (*_node)(w).AsFloat()
	}
	return 0, datamodel.ErrWrongKind{
		TypeName:        w.schemaType.Name(),
		MethodName:      "AsFloat",
		AppropriateKind: datamodel.KindSet_JustFloat,
		ActualKind:      w.Kind(),
	}
}

func (w *_nodeRepr) AsString() (string, error) {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Stringjoin:
		var b strings.Builder
		itr := (*_node)(w).MapIterator()
		first := true
		for !itr.Done() {
			_, v, err := itr.Next()
			if err != nil {
				return "", err
			}
			s, err := reprNode(v).AsString()
			if err != nil {
				return "", err
			}
			if first {
				first = false
			} else {
				b.WriteString(stg.GetDelim())
			}
			b.WriteString(s)
		}
		return b.String(), nil
	case schema.UnionRepresentation_Stringprefix:
		haveIdx, mval := unionMember(w.val)
		mtyp := w.schemaType.(*schema.TypeUnion).Members()[haveIdx]

		w2 := *w
		w2.val = mval
		w2.schemaType = mtyp
		s, err := w2.AsString()
		if err != nil {
			return "", err
		}

		name := stg.GetDiscriminant(mtyp)
		return name + stg.GetDelim() + s, nil
	case schema.UnionRepresentation_Kinded:
		return w.asKinded(stg, datamodel.Kind_String).AsString()
	case nil:
		// continues below
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
	// Things that have reached here, should be only those things that have string kind naturally,
	// and so using the same method as the type level is correct.
	// TODO is this applicable for anything other than... strings?
	return (*_node)(w).AsString()
}

func (w *_nodeRepr) AsBytes() ([]byte, error) {
	if reprStrategy(w.schemaType) == nil {
		return (*_node)(w).AsBytes()
	}
	return nil, datamodel.ErrWrongKind{
		TypeName:        w.schemaType.Name(),
		MethodName:      "AsBytes",
		AppropriateKind: datamodel.KindSet_JustBytes,
		ActualKind:      w.Kind(),
	}
}

func (w *_nodeRepr) AsLink() (datamodel.Link, error) {
	if reprStrategy(w.schemaType) == nil {
		return (*_node)(w).AsLink()
	}
	return nil, datamodel.ErrWrongKind{
		TypeName:        w.schemaType.Name(),
		MethodName:      "AsLink",
		AppropriateKind: datamodel.KindSet_JustLink,
		ActualKind:      w.Kind(),
	}
}

func (w *_nodeRepr) Prototype() datamodel.NodePrototype {
	panic("TODO: Prototype")
}

type _builderRepr struct {
	_assemblerRepr
}

// TODO: returning a repr node here is probably good, but there's a gotcha: one
// can go from a typed node to a repr node via the Representation method, but
// not the other way. That's probably why codegen returns a typed node here.
// The solution might be to add a way to go from the repr node to its parent
// typed node.

func (w *_builderRepr) Build() datamodel.Node {
	// TODO: see the notes above.
	// return &_nodeRepr{schemaType: w.schemaType, val: w.val}
	return &_node{schemaType: w.schemaType, val: w.val}
}

func (w *_builderRepr) Reset() {
	panic("TODO: Reset")
}

type _assemblerRepr struct {
	schemaType schema.Type
	val        reflect.Value // non-pointer
	finish     func() error

	nullable bool
}

func assemblerRepr(am datamodel.NodeAssembler) *_assemblerRepr {
	return (*_assemblerRepr)(am.(*_assembler))
}

func (w *_assemblerRepr) asKinded(stg schema.UnionRepresentation_Kinded, kind datamodel.Kind) *_assemblerRepr {
	name := stg.GetMember(kind)
	members := w.schemaType.(*schema.TypeUnion).Members()
	for idx, member := range members {
		if member.Name() != name {
			continue
		}
		w2 := *w
		goType := w.val.Field(idx).Type().Elem()
		valPtr := reflect.New(goType)
		w2.val = valPtr.Elem()
		w2.schemaType = member

		// Layer a new finish func on top, to set Index/Value.
		w2.finish = func() error {
			unionSetMember(w.val, idx, valPtr)
			if w.finish != nil {
				if err := w.finish(); err != nil {
					return err
				}
			}
			return nil
		}
		return &w2
	}
	return nil
}

func (w *_assemblerRepr) BeginMap(sizeHint int64) (datamodel.MapAssembler, error) {
	if stg, ok := reprStrategy(w.schemaType).(schema.UnionRepresentation_Kinded); ok {
		w = w.asKinded(stg, datamodel.Kind_Map)
	}
	asm, err := (*_assembler)(w).BeginMap(sizeHint)
	if err != nil {
		return nil, err
	}
	switch asm := asm.(type) {
	case *_structAssembler:
		return (*_structAssemblerRepr)(asm), nil
	case *_mapAssembler:
		return (*_mapAssemblerRepr)(asm), nil
	case *_unionAssembler:
		return (*_unionAssemblerRepr)(asm), nil
	default:
		panic(fmt.Sprintf("%T", asm))
	}
}

func (w *_assemblerRepr) BeginList(sizeHint int64) (datamodel.ListAssembler, error) {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Tuple:
		asm, err := (*_assembler)(w).BeginMap(sizeHint)
		if err != nil {
			return nil, err
		}
		return (*_listStructAssemblerRepr)(asm.(*_structAssembler)), nil
	case nil:
		asm, err := (*_assembler)(w).BeginList(sizeHint)
		if err != nil {
			return nil, err
		}
		return (*_listAssemblerRepr)(asm.(*_listAssembler)), nil
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) AssignNull() error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		return (*_assembler)(w).AssignNull()
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) AssignBool(b bool) error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.UnionRepresentation_Kinded:
		return w.asKinded(stg, datamodel.Kind_Bool).AssignBool(b)
	case nil:
		return (*_assembler)(w).AssignBool(b)
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) AssignInt(i int64) error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		return (*_assembler)(w).AssignInt(i)
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) AssignFloat(f float64) error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		return (*_assembler)(w).AssignFloat(f)
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) AssignString(s string) error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Stringjoin:
		fields := w.schemaType.(*schema.TypeStruct).Fields()
		parts := strings.Split(s, stg.GetDelim())
		if len(parts) != len(fields) {
			panic("TODO: len mismatch")
		}
		mapAsm, err := (*_assembler)(w).BeginMap(-1)
		if err != nil {
			return err
		}
		for i, field := range fields {
			entryAsm, err := mapAsm.AssembleEntry(field.Name())
			if err != nil {
				return err
			}
			entryAsm = assemblerRepr(entryAsm)
			if err := entryAsm.AssignString(parts[i]); err != nil {
				return err
			}
		}
		return mapAsm.Finish()
	case schema.UnionRepresentation_Kinded:
		return w.asKinded(stg, datamodel.Kind_String).AssignString(s)
	case schema.UnionRepresentation_Stringprefix:
		hasDelim := stg.GetDelim() != ""

		var prefix, remainder string
		if hasDelim {
			parts := strings.SplitN(s, stg.GetDelim(), 2)
			if len(parts) != 2 {
				return fmt.Errorf("schema rejects data: the union type %s expects delimiter %q, and it was not found in the data %q", w.schemaType.Name(), stg.GetDelim(), s)
			}
			prefix, remainder = parts[0], parts[1]
		}

		members := w.schemaType.(*schema.TypeUnion).Members()
		for idx, member := range members {
			descrm := stg.GetDiscriminant(member)
			if hasDelim {
				if stg.GetDiscriminant(member) != prefix {
					continue
				}
			} else {
				if !strings.HasPrefix(s, descrm) {
					continue
				}
				remainder = s[len(descrm):]
			}

			// TODO: DRY: this has much in common with the asKinded method; it differs only in that we picked idx already in a different way.
			w2 := *w
			goType := w.val.Field(idx).Type().Elem()
			valPtr := reflect.New(goType)
			w2.val = valPtr.Elem()
			w2.schemaType = member
			w2.finish = func() error {
				unionSetMember(w.val, idx, valPtr)
				if w.finish != nil {
					if err := w.finish(); err != nil {
						return err
					}
				}
				return nil
			}
			return w2.AssignString(remainder)
		}
		return fmt.Errorf("schema rejects data: the union type %s requires a known prefix, and it was not found in the data %q", w.schemaType.Name(), s)
	case nil:
		return (*_assembler)(w).AssignString(s)
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) AssignBytes(p []byte) error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		return (*_assembler)(w).AssignBytes(p)
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) AssignLink(link datamodel.Link) error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		return (*_assembler)(w).AssignLink(link)
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) AssignNode(node datamodel.Node) error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		return (*_assembler)(w).AssignNode(node)
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_assemblerRepr) Prototype() datamodel.NodePrototype {
	panic("TODO: Assembler.Prototype")
}

type _structAssemblerRepr _structAssembler

func (w *_structAssemblerRepr) AssembleKey() datamodel.NodeAssembler {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Map:
		return (*_structAssembler)(w).AssembleKey()
	case schema.StructRepresentation_Stringjoin,
		schema.StructRepresentation_StringPairs:
		// TODO an error-carrying "NodeAssembler" is needed so that this can refrain from panicking.
		// TODO perhaps the ErrorWrongKind type should also be extended to explicitly describe whether the method was applied on bare DM, type-level, or repr-level.
		panic(&datamodel.ErrWrongKind{
			TypeName:        w.schemaType.Name() + ".Repr",
			MethodName:      "AssembleKey",
			AppropriateKind: datamodel.KindSet_JustMap,
			ActualKind:      datamodel.Kind_String,
		})
	case schema.StructRepresentation_Tuple:
		panic(&datamodel.ErrWrongKind{
			TypeName:        w.schemaType.Name() + ".Repr",
			MethodName:      "AssembleKey",
			AppropriateKind: datamodel.KindSet_JustMap,
			ActualKind:      datamodel.Kind_List,
		})
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_structAssemblerRepr) AssembleValue() datamodel.NodeAssembler {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Map:
		key := w.curKey.val.String()
		revKey := inboundMappedKey(w.schemaType, stg, key)
		w.curKey.val.SetString(revKey)

		valAsm := (*_structAssembler)(w).AssembleValue()
		valAsm = assemblerRepr(valAsm)
		return valAsm
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_structAssemblerRepr) AssembleEntry(k string) (datamodel.NodeAssembler, error) {
	if err := w.AssembleKey().AssignString(k); err != nil {
		return nil, err
	}
	am := w.AssembleValue()
	return am, nil
}

func (w *_structAssemblerRepr) Finish() error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Map:
		err := (*_structAssembler)(w).Finish()
		if err, ok := err.(schema.ErrMissingRequiredField); ok {
			for i, name := range err.Missing {
				serial := outboundMappedKey(stg, name)
				if serial != name {
					err.Missing[i] += fmt.Sprintf(" (serial:%q)", serial)
				}
			}
		}
		return err
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_structAssemblerRepr) KeyPrototype() datamodel.NodePrototype {
	panic("TODO")
}

func (w *_structAssemblerRepr) ValuePrototype(k string) datamodel.NodePrototype {
	panic("TODO: struct ValuePrototype")
}

type _mapAssemblerRepr _mapAssembler

func (w *_mapAssemblerRepr) AssembleKey() datamodel.NodeAssembler {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		asm := (*_mapAssembler)(w).AssembleKey()
		return (*_assemblerRepr)(asm.(*_assembler))
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_mapAssemblerRepr) AssembleValue() datamodel.NodeAssembler {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		asm := (*_mapAssembler)(w).AssembleValue()
		return (*_assemblerRepr)(asm.(*_assembler))
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_mapAssemblerRepr) AssembleEntry(k string) (datamodel.NodeAssembler, error) {
	if err := w.AssembleKey().AssignString(k); err != nil {
		return nil, err
	}
	am := w.AssembleValue()
	return am, nil
}

func (w *_mapAssemblerRepr) Finish() error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case nil:
		return (*_mapAssembler)(w).Finish()
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_mapAssemblerRepr) KeyPrototype() datamodel.NodePrototype {
	panic("TODO")
}

func (w *_mapAssemblerRepr) ValuePrototype(k string) datamodel.NodePrototype {
	panic("TODO: struct ValuePrototype")
}

type _listStructAssemblerRepr _structAssembler

func (w *_listStructAssemblerRepr) AssembleValue() datamodel.NodeAssembler {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Tuple:
		fields := w.schemaType.Fields()
		field := fields[w.nextIndex]
		w.nextIndex++

		entryAsm, err := (*_structAssembler)(w).AssembleEntry(field.Name())
		if err != nil {
			panic(err) // TODO: probably return an assembler that always errors?
		}
		entryAsm = assemblerRepr(entryAsm)
		return entryAsm
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_listStructAssemblerRepr) Finish() error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Tuple:
		return (*_structAssembler)(w).Finish()
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_listStructAssemblerRepr) ValuePrototype(idx int64) datamodel.NodePrototype {
	panic("TODO: list ValuePrototype")
}

// Note that lists do not have any representation strategy right now.
type _listAssemblerRepr _listAssembler

func (w *_listAssemblerRepr) AssembleValue() datamodel.NodeAssembler {
	asm := (*_listAssembler)(w).AssembleValue()
	return (*_assemblerRepr)(asm.(*_assembler))
}

func (w *_listAssemblerRepr) Finish() error {
	return (*_listAssembler)(w).Finish()
}

func (w *_listAssemblerRepr) ValuePrototype(idx int64) datamodel.NodePrototype {
	panic("TODO: list ValuePrototype")
}

type _unionAssemblerRepr _unionAssembler

func (w *_unionAssemblerRepr) AssembleKey() datamodel.NodeAssembler {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.UnionRepresentation_Keyed:
		return (*_unionAssembler)(w).AssembleKey()
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_unionAssemblerRepr) AssembleValue() datamodel.NodeAssembler {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.UnionRepresentation_Keyed:
		key := w.curKey.val.String()
		revKey := inboundMappedType(w.schemaType, stg, key)
		w.curKey.val.SetString(revKey)

		valAsm := (*_unionAssembler)(w).AssembleValue()
		valAsm = assemblerRepr(valAsm)
		return valAsm
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_unionAssemblerRepr) AssembleEntry(k string) (datamodel.NodeAssembler, error) {
	if err := w.AssembleKey().AssignString(k); err != nil {
		return nil, err
	}
	am := w.AssembleValue()
	return am, nil
}

func (w *_unionAssemblerRepr) Finish() error {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.UnionRepresentation_Keyed:
		return (*_unionAssembler)(w).Finish()
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_unionAssemblerRepr) KeyPrototype() datamodel.NodePrototype {
	panic("TODO")
}

func (w *_unionAssemblerRepr) ValuePrototype(k string) datamodel.NodePrototype {
	panic("TODO: struct ValuePrototype")
}

type _structIteratorRepr _structIterator

func (w *_structIteratorRepr) Next() (key, value datamodel.Node, _ error) {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Map:
	_skipAbsent:
		key, value, err := (*_structIterator)(w).Next()
		if err != nil {
			return nil, nil, err
		}
		if value.IsAbsent() {
			goto _skipAbsent
		}
		keyStr, _ := key.AsString()
		mappedKey := outboundMappedKey(stg, keyStr)
		if mappedKey != keyStr {
			key = basicnode.NewString(mappedKey)
		}
		return key, reprNode(value), nil
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_structIteratorRepr) Done() bool {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.StructRepresentation_Map:
		// TODO: the fact that repr map iterators skip absents should be
		// documented somewhere
		return w.nextIndex >= w.reprEnd
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

type _unionIteratorRepr _unionIterator

func (w *_unionIteratorRepr) Next() (key, value datamodel.Node, _ error) {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.UnionRepresentation_Keyed:
		key, value, err := (*_unionIterator)(w).Next()
		if err != nil {
			return nil, nil, err
		}
		keyStr, _ := key.AsString()
		mappedKey := outboundMappedType(stg, keyStr)
		if mappedKey != keyStr {
			key = basicnode.NewString(mappedKey)
		}
		return key, reprNode(value), nil
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}

func (w *_unionIteratorRepr) Done() bool {
	switch stg := reprStrategy(w.schemaType).(type) {
	case schema.UnionRepresentation_Keyed:
		return (*_unionIterator)(w).Done()
	default:
		panic(fmt.Sprintf("TODO: %T", stg))
	}
}
