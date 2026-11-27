package sceneformat_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/entities"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

type testHealth struct {
	HP   int
	Max  int
	Tags []string
}

type testSpeed struct {
	Speed float64
}

func TestSpawnEntitiesWithDefaultsAndCloneIsolation(t *testing.T) {
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "health",
		Fields: []sceneformat.Field{
			{Name: "hp", Kind: sceneformat.FieldInt, Default: 10, Min: sceneformat.Float64(0), Max: sceneformat.Float64(100)},
			{Name: "max", Kind: sceneformat.FieldInt, Default: 10, Min: sceneformat.Float64(1), Max: sceneformat.Float64(100)},
			{Name: "tags", Kind: sceneformat.FieldStringList, Default: []string{"living"}},
		},
		Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
			tags := fields["tags"].([]string)
			entities.Set(w, e, testHealth{
				HP:   fields["hp"].(int),
				Max:  fields["max"].(int),
				Tags: append([]string(nil), tags...),
			})
			// Mutate fields in place to verify each Spawn call receives its own resolved copy.
			tags[0] = "mutated"
			fields["hp"] = -999
			return nil
		},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name: "mover",
		Fields: []sceneformat.Field{
			{Name: "speed", Kind: sceneformat.FieldFloat, Default: 12.5},
		},
		Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
			entities.Set(w, e, testSpeed{Speed: fields["speed"].(float64)})
			return nil
		},
	})
	// A registered component without Spawn is validated and otherwise skipped.
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "marker",
		Fields: []sceneformat.Field{{Name: "label", Kind: sceneformat.FieldString, Default: "m"}},
	})

	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: actors
    objects:
      - id: a
        components:
          health: {}
          mover:
            speed: 4
          marker: {}
      - id: b
        components:
          health:
            hp: 3
            max: 20
          mover: {}
      - id: plain
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}

	w := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(doc, w, sceneformat.SpawnOptions{Components: reg})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith: %v", err)
	}
	if len(spawned) != 3 || w.Count() != 3 {
		t.Fatalf("spawned %d (world count %d), want 3", len(spawned), w.Count())
	}

	ha, ok := entities.Get[testHealth](w, spawned["a"])
	if !ok || !reflect.DeepEqual(*ha, testHealth{HP: 10, Max: 10, Tags: []string{"living"}}) {
		t.Fatalf("a health = %+v (%v), want default {10 10 [living]}", ha, ok)
	}
	hb, ok := entities.Get[testHealth](w, spawned["b"])
	if !ok || !reflect.DeepEqual(*hb, testHealth{HP: 3, Max: 20, Tags: []string{"living"}}) {
		t.Fatalf("b health = %+v (%v), want {3 20 [living]} (defaults must not be mutated by earlier Spawn)", hb, ok)
	}
	sa, ok := entities.Get[testSpeed](w, spawned["a"])
	if !ok || sa.Speed != 4.0 {
		t.Fatalf("a speed = %+v (%v), want 4.0 (int widened to float64)", sa, ok)
	}
	sb, ok := entities.Get[testSpeed](w, spawned["b"])
	if !ok || sb.Speed != 12.5 {
		t.Fatalf("b speed = %+v (%v), want default 12.5", sb, ok)
	}
	if entities.Has[testHealth](w, spawned["plain"]) || entities.Has[testSpeed](w, spawned["plain"]) {
		t.Fatal("plain object unexpectedly received spawned components")
	}

	// Lookup and Types preserve Spawn on cloned ComponentType values.
	ct, ok := reg.Lookup("mover")
	if !ok || ct.Spawn == nil {
		t.Fatal("Lookup did not preserve ComponentType.Spawn")
	}
	reg2 := sceneformat.NewComponentRegistry()
	reg2.MustRegister(ct)
	doc2, err := sceneformat.ParseWith([]byte("version: 1\nlayers:\n  - name: a\n    objects:\n      - id: x\n        components:\n          mover: {}\n"), sceneformat.ParseOptions{Components: reg2})
	if err != nil {
		t.Fatalf("ParseWith reg2: %v", err)
	}
	w2 := entities.NewWorld()
	sp2, err := sceneformat.SpawnEntitiesWith(doc2, w2, sceneformat.SpawnOptions{Components: reg2})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith reg2: %v", err)
	}
	if s, ok := entities.Get[testSpeed](w2, sp2["x"]); !ok || s.Speed != 12.5 {
		t.Fatalf("cloned mover Spawn gave %+v (%v), want 12.5", s, ok)
	}
}

func TestSpawnEntitiesWithDeterministicOrderAndNestedChildren(t *testing.T) {
	var order []string
	var childSawParent bool
	var childSawParentComp bool

	type parentTag struct{}

	reg := sceneformat.NewComponentRegistry()
	record := func(name string) sceneformat.ComponentType {
		return sceneformat.ComponentType{
			Name: name,
			Spawn: func(w *entities.World, e entities.Entity, _ map[string]any) error {
				ref, ok := entities.Get[sceneformat.ObjectRef](w, e)
				if !ok {
					t.Errorf("%s Spawn: missing ObjectRef on entity %v", name, e)
				}
				order = append(order, ref.Layer+"/"+ref.ID+"."+name)
				if ref.ID == "root" && name == "alpha" {
					entities.Set(w, e, parentTag{})
				}
				if ref.ID == "child1" && name == "alpha" {
					if p, has := w.ParentOf(e); has {
						if pref, ok := entities.Get[sceneformat.ObjectRef](w, p); ok && pref.ID == "root" {
							childSawParent = true
							childSawParentComp = entities.Has[parentTag](w, p)
						}
					}
				}
				return nil
			},
		}
	}
	reg.MustRegister(record("zeta"))
	reg.MustRegister(record("alpha"))
	reg.MustRegister(record("mid"))

	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: l1
    objects:
      - id: root
        x: 10
        "y": 20
        components:
          zeta: {}
          alpha: {}
          mid: {}
        children:
          - id: child1
            x: 5
            components:
              mid: {}
              alpha: {}
            children:
              - id: grand
                x: 2
                components:
                  zeta: {}
          - id: child2
            components:
              alpha: {}
      - id: sibling
        components:
          zeta: {}
          alpha: {}
  - name: l2
    objects:
      - id: other
        components:
          mid: {}
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatalf("ParseWith: %v", err)
	}

	w := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(doc, w, sceneformat.SpawnOptions{Components: reg})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith: %v", err)
	}
	want := []string{
		"l1/root.alpha",
		"l1/root.mid",
		"l1/root.zeta",
		"l1/child1.alpha",
		"l1/child1.mid",
		"l1/grand.zeta",
		"l1/child2.alpha",
		"l1/sibling.alpha",
		"l1/sibling.zeta",
		"l2/other.mid",
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("spawn order =\n  %v\nwant:\n  %v", order, want)
	}
	if !childSawParent || !childSawParentComp {
		t.Fatalf("child1 Spawn saw parent=%v parentComp=%v, want both true", childSawParent, childSawParentComp)
	}

	h := w.Hierarchy2D()
	h.Sync()
	wt, ok := h.WorldTransform(spawned["grand"])
	if !ok || !near(wt.X, 17) || !near(wt.Y, 20) || !wt.WorldVisible {
		t.Fatalf("grand world transform = %+v (%v), want (17, 20) visible", wt, ok)
	}
}

func TestSpawnEntitiesWithHiddenLayers(t *testing.T) {
	visible := false
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "health",
		Fields: []sceneformat.Field{{Name: "hp", Kind: sceneformat.FieldInt, Default: 7}},
		Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
			entities.Set(w, e, testHealth{HP: fields["hp"].(int), Max: 10})
			return nil
		},
	})

	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{
			{
				Name:    "secret",
				Visible: &visible,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:         "vault",
						X:          10,
						Y:          20,
						Components: map[string]any{"health": map[string]any{}},
						Children: []sceneformat.ObjectDoc{
							{
								ID:         "relic",
								X:          5,
								Components: map[string]any{"health": map[string]any{"hp": 3}},
							},
						},
					},
				},
			},
		},
	}

	w := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(doc, w, sceneformat.SpawnOptions{Components: reg})
	if err != nil {
		t.Fatalf("SpawnEntitiesWith: %v", err)
	}
	h := w.Hierarchy2D()
	h.Sync()
	for _, tc := range []struct {
		id     string
		wantHP int
		hidden bool
	}{
		{"vault", 7, true},
		{"relic", 3, false},
	} {
		e := spawned[tc.id]
		hp, ok := entities.Get[testHealth](w, e)
		if !ok || hp.HP != tc.wantHP {
			t.Errorf("%q health = %+v (%v), want hp %d", tc.id, hp, ok, tc.wantHP)
		}
		lt, _ := h.Local(e)
		if lt.Hidden != tc.hidden {
			t.Errorf("%q local Hidden = %v, want %v", tc.id, lt.Hidden, tc.hidden)
		}
		wt, _ := h.WorldTransform(e)
		if wt.WorldVisible {
			t.Errorf("%q WorldVisible = true, want false on hidden layer", tc.id)
		}
	}
}

func TestSpawnEntitiesWithValidatesBeforeSpawningAnything(t *testing.T) {
	spawnCalls := 0
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "health",
		Fields: []sceneformat.Field{{Name: "hp", Kind: sceneformat.FieldInt, Default: 5, Min: sceneformat.Float64(0)}},
		Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
			spawnCalls++
			entities.Set(w, e, testHealth{HP: fields["hp"].(int)})
			return nil
		},
	})

	cases := []struct {
		name    string
		objects []sceneformat.ObjectDoc
		wantObj string
		wantSub string
	}{
		{
			name: "unregistered component on later object",
			objects: []sceneformat.ObjectDoc{
				{ID: "first", Components: map[string]any{"health": map[string]any{"hp": 4}}},
				{ID: "second", Components: map[string]any{"unknown": map[string]any{}}},
			},
			wantObj: "second",
			wantSub: "unknown component type",
		},
		{
			name: "invalid field value on nested child",
			objects: []sceneformat.ObjectDoc{
				{
					ID:         "first",
					Components: map[string]any{"health": map[string]any{"hp": 4}},
					Children: []sceneformat.ObjectDoc{
						{ID: "badchild", Components: map[string]any{"health": map[string]any{"hp": -1}}},
					},
				},
			},
			wantObj: "badchild",
			wantSub: "below the minimum",
		},
		{
			name: "null component value",
			objects: []sceneformat.ObjectDoc{
				{ID: "first", Components: map[string]any{"health": map[string]any{}}},
				{ID: "nullcomp", Components: map[string]any{"health": nil}},
			},
			wantObj: "nullcomp",
			wantSub: "must be a mapping",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spawnCalls = 0
			doc := &sceneformat.SceneDoc{
				Version: sceneformat.CurrentVersion,
				Layers:  []sceneformat.LayerDoc{{Name: "actors", Objects: tc.objects}},
			}
			w := entities.NewWorld()
			pre := w.Spawn()
			spawned, err := sceneformat.SpawnEntitiesWith(doc, w, sceneformat.SpawnOptions{Components: reg})
			if err == nil {
				t.Fatal("SpawnEntitiesWith succeeded on invalid component")
			}
			var ce *sceneformat.ComponentError
			if !errors.As(err, &ce) || ce.Object != tc.wantObj || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %v, want *ComponentError for %q containing %q", err, tc.wantObj, tc.wantSub)
			}
			if spawned != nil {
				t.Fatalf("spawned = %v, want nil", spawned)
			}
			if spawnCalls != 0 {
				t.Fatalf("Spawn was called %d times before validation failed, want 0", spawnCalls)
			}
			if w.Count() != 1 || !w.Alive(pre) {
				t.Fatalf("world count = %d (pre alive=%v), want 1 with pre untouched", w.Count(), w.Alive(pre))
			}
		})
	}
}

func TestSpawnEntitiesWithRollbackOnSpawnError(t *testing.T) {
	errBoom := errors.New("broken reactor")

	for _, policy := range []struct {
		name string
		p    entities.RemovePolicy
	}{
		{"detach", entities.RemoveDetach},
		{"detach_keep_world", entities.RemoveDetachKeepWorld},
		{"cascade", entities.RemoveCascade},
	} {
		t.Run(policy.name, func(t *testing.T) {
			var ran []string
			reg := sceneformat.NewComponentRegistry()
			reg.MustRegister(sceneformat.ComponentType{
				Name: "alpha",
				Spawn: func(w *entities.World, e entities.Entity, _ map[string]any) error {
					ref, _ := entities.Get[sceneformat.ObjectRef](w, e)
					ran = append(ran, ref.ID+".alpha")
					entities.Set(w, e, testHealth{HP: 1})
					return nil
				},
			})
			reg.MustRegister(sceneformat.ComponentType{
				Name: "beta",
				Spawn: func(w *entities.World, e entities.Entity, _ map[string]any) error {
					ref, _ := entities.Get[sceneformat.ObjectRef](w, e)
					ran = append(ran, ref.ID+".beta")
					if ref.ID == "turret" {
						return errBoom
					}
					entities.Set(w, e, testSpeed{Speed: 2})
					return nil
				},
			})

			doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: ships
    objects:
      - id: hull
        x: 10
        components:
          alpha: {}
          beta: {}
        children:
          - id: turret
            x: 4
            components:
              alpha: {}
              beta: {}
          - id: never
            components:
              alpha: {}
      - id: after
        components:
          alpha: {}
`), sceneformat.ParseOptions{Components: reg})
			if err != nil {
				t.Fatalf("ParseWith: %v", err)
			}

			w := entities.NewWorld()
			w.Hierarchy2D().SetRemovePolicy(policy.p)
			pre := w.Spawn()
			entities.Set(w, pre, testHealth{HP: 99})

			spawned, err := sceneformat.SpawnEntitiesWith(doc, w, sceneformat.SpawnOptions{Components: reg})
			if err == nil {
				t.Fatal("SpawnEntitiesWith succeeded when beta failed")
			}
			if spawned != nil {
				t.Fatalf("spawned = %v, want nil", spawned)
			}
			if !errors.Is(err, errBoom) {
				t.Fatalf("err = %v, want it to wrap %v", err, errBoom)
			}
			for _, sub := range []string{`layer "ships"`, `object "turret"`, `component "beta"`} {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("err %q does not contain %q", err, sub)
				}
			}
			wantRan := []string{"hull.alpha", "hull.beta", "turret.alpha", "turret.beta"}
			if !reflect.DeepEqual(ran, wantRan) {
				t.Fatalf("ran = %v, want %v (objects after failure must not spawn)", ran, wantRan)
			}
			if w.Count() != 1 || !w.Alive(pre) {
				t.Fatalf("world count = %d (pre alive=%v), want 1 (all spawned entities rolled back)", w.Count(), w.Alive(pre))
			}
			if hp, ok := entities.Get[testHealth](w, pre); !ok || hp.HP != 99 {
				t.Fatalf("pre-existing entity health = %+v (%v), want 99", hp, ok)
			}
			if entities.Component[testHealth](w).Len() != 1 || entities.Component[testSpeed](w).Len() != 0 || entities.Component[sceneformat.ObjectRef](w).Len() != 0 {
				t.Fatalf("stores not clean after rollback: health=%d speed=%d refs=%d",
					entities.Component[testHealth](w).Len(),
					entities.Component[testSpeed](w).Len(),
					entities.Component[sceneformat.ObjectRef](w).Len(),
				)
			}
		})
	}
}

func TestSpawnEntitiesWithDisposedAndNilWorld(t *testing.T) {
	called := false
	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name: "health",
		Spawn: func(w *entities.World, e entities.Entity, _ map[string]any) error {
			called = true
			return nil
		},
	})
	doc, err := sceneformat.ParseWith([]byte("version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          health: {}\n"), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatal(err)
	}

	disposed := entities.NewWorld()
	disposed.Dispose()
	if _, err := sceneformat.SpawnEntitiesWith(doc, disposed, sceneformat.SpawnOptions{Components: reg}); !errors.Is(err, entities.ErrWorldDisposed) {
		t.Fatalf("disposed world err = %v, want ErrWorldDisposed", err)
	}
	if _, err := sceneformat.SpawnEntitiesWith(doc, nil, sceneformat.SpawnOptions{Components: reg}); err == nil {
		t.Fatal("nil world succeeded")
	}
	if called {
		t.Fatal("Spawn was called on a nil or disposed world")
	}

	// Nil Components keeps SpawnEntities's free-form behavior.
	freeDoc, err := sceneformat.Parse([]byte("version: 1\nlayers:\n  - name: a\n    objects:\n      - id: p\n        components:\n          anything: {x: 1}\n"))
	if err != nil {
		t.Fatal(err)
	}
	w := entities.NewWorld()
	spawned, err := sceneformat.SpawnEntitiesWith(freeDoc, w, sceneformat.SpawnOptions{})
	if err != nil || len(spawned) != 1 {
		t.Fatalf("SpawnEntitiesWith(nil registry) = %v, %v; want 1 entity", spawned, err)
	}
}

func TestSpawnConnectedRunsComponentSpawnAndRollsBackOnError(t *testing.T) {
	type doorState struct {
		Open bool
	}

	reg := sceneformat.NewComponentRegistry()
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name:   "door",
		Fields: []sceneformat.Field{{Name: "open", Kind: sceneformat.FieldBool, Default: false}},
		Handlers: []sceneformat.Handler{{
			Name: "open",
			Func: func(c sceneformat.Call) {
				w := c.Context.(*entities.World)
				if st, ok := entities.Get[doorState](w, c.TargetEntity); ok {
					st.Open = true
				}
			},
		}},
		Spawn: func(w *entities.World, e entities.Entity, fields map[string]any) error {
			entities.Set(w, e, doorState{Open: fields["open"].(bool)})
			return nil
		},
	})

	doc, err := sceneformat.ParseWith([]byte(`version: 1
layers:
  - name: props
    objects:
      - id: btn
        components:
          button: {}
      - id: gate
        components:
          door: {}
connections:
  - from: btn
    event: pressed
    to: gate
    handler: open
`), sceneformat.ParseOptions{Components: reg})
	if err != nil {
		t.Fatal(err)
	}

	w := entities.NewWorld()
	spawned, sig, err := sceneformat.SpawnConnected(doc, w, reg, w)
	if err != nil {
		t.Fatalf("SpawnConnected: %v", err)
	}
	st, ok := entities.Get[doorState](w, spawned["gate"])
	if !ok || st.Open {
		t.Fatalf("initial doorState = %+v (%v), want {Open:false}", st, ok)
	}
	if err := sig.EmitEntity(spawned["btn"], "pressed", nil); err != nil {
		t.Fatalf("EmitEntity: %v", err)
	}
	if st, _ := entities.Get[doorState](w, spawned["gate"]); !st.Open {
		t.Fatal("handler did not flip doorState spawned by ComponentType.Spawn")
	}

	// A Spawn error inside SpawnConnected rolls back all entities created so far.
	errFail := errors.New("door jammed")
	failReg := sceneformat.NewComponentRegistry()
	failReg.MustRegister(sceneformat.ComponentType{
		Name:   "button",
		Events: []sceneformat.Event{{Name: "pressed"}},
	})
	failReg.MustRegister(sceneformat.ComponentType{
		Name:     "door",
		Fields:   []sceneformat.Field{{Name: "open", Kind: sceneformat.FieldBool}},
		Handlers: []sceneformat.Handler{{Name: "open", Func: func(sceneformat.Call) {}}},
		Spawn: func(*entities.World, entities.Entity, map[string]any) error {
			return errFail
		},
	})
	w2 := entities.NewWorld()
	sp2, sig2, err := sceneformat.SpawnConnected(doc, w2, failReg, nil)
	if !errors.Is(err, errFail) || sp2 != nil || sig2 != nil || w2.Count() != 0 {
		t.Fatalf("SpawnConnected on Spawn error = %v, %v, %v (count %d); want errFail and 0 entities", sp2, sig2, err, w2.Count())
	}
	if !strings.Contains(err.Error(), `layer "props"`) || !strings.Contains(err.Error(), `object "gate"`) || !strings.Contains(err.Error(), `component "door"`) {
		t.Fatalf("SpawnConnected error %q missing layer/object/component context", err)
	}
}
