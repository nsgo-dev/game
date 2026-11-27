//go:build js && wasm

package leaderboard

import (
	"fmt"
	"sync"
	"syscall/js"
)

type jsMiniGameHost struct {
	bridge js.Value
	api    js.Value
}

// defaultMiniGameHost discovers WeChat (wx) or Douyin (tt) mini-game friend storage and open data context APIs through NSGo's nsgoMP.api bridge or the host globals.
func defaultMiniGameHost() MiniGameHost {
	g := js.Global()
	if !g.Truthy() {
		return nil
	}
	bridge := g.Get("nsgoMP")
	if bridge.Type() == js.TypeObject && !bridge.IsNull() {
		api := bridge.Get("api")
		if hasCloudStorageAPI(api) {
			return &jsMiniGameHost{bridge: bridge, api: api}
		}
	}
	for _, name := range []string{"wx", "tt"} {
		api := g.Get(name)
		if hasCloudStorageAPI(api) {
			return &jsMiniGameHost{bridge: bridge, api: api}
		}
	}
	return nil
}

func hasCloudStorageAPI(api js.Value) bool {
	return api.Type() == js.TypeObject && !api.IsNull() && api.Get("setUserCloudStorage").Type() == js.TypeFunction
}

func (h *jsMiniGameHost) SetUserCloudStorage(entries []MiniGameEntry, done func(error)) {
	if !hasCloudStorageAPI(h.api) {
		if done != nil {
			done(ErrUnsupported)
		}
		return
	}
	kvList := js.Global().Get("Array").New(len(entries))
	for i, e := range entries {
		kvList.SetIndex(i, map[string]any{
			"key":   e.Key,
			"value": e.Value,
		})
	}
	var once sync.Once
	var successFn, failFn js.Func
	finish := func(err error) {
		once.Do(func() {
			successFn.Release()
			failFn.Release()
			if done != nil {
				done(err)
			}
		})
	}
	successFn = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		finish(nil)
		return nil
	})
	failFn = js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "setUserCloudStorage failed"
		if len(args) > 0 && args[0].Type() == js.TypeObject && !args[0].IsNull() {
			if m := args[0].Get("errMsg"); m.Type() == js.TypeString && m.String() != "" {
				msg = m.String()
			}
		}
		finish(fmt.Errorf("%w: %s", ErrUnavailable, msg))
		return nil
	})
	h.api.Call("setUserCloudStorage", map[string]any{
		"KVDataList": kvList,
		"success":    successFn,
		"fail":       failFn,
	})
}

func (h *jsMiniGameHost) ShowFriendRanking(req MiniGameRanking) error {
	payload := map[string]any{
		"type":   "nsgo.leaderboard.show",
		"key":    req.Key,
		"order":  string(req.Order),
		"x":      float64(req.Bounds.Origin.X),
		"y":      float64(req.Bounds.Origin.Y),
		"width":  float64(req.Bounds.Size.Width),
		"height": float64(req.Bounds.Size.Height),
	}
	if h.bridge.Type() == js.TypeObject && !h.bridge.IsNull() && h.bridge.Get("showOpenDataContext").Type() == js.TypeFunction {
		h.bridge.Call("showOpenDataContext", payload)
		return nil
	}
	return h.postOpenDataMessage(payload)
}

func (h *jsMiniGameHost) HideFriendRanking() error {
	if h.bridge.Type() == js.TypeObject && !h.bridge.IsNull() && h.bridge.Get("hideOpenDataContext").Type() == js.TypeFunction {
		h.bridge.Call("hideOpenDataContext")
		return nil
	}
	return h.postOpenDataMessage(map[string]any{
		"type": "nsgo.leaderboard.hide",
	})
}

func (h *jsMiniGameHost) postOpenDataMessage(payload map[string]any) error {
	if h.api.Type() != js.TypeObject || h.api.IsNull() || h.api.Get("getOpenDataContext").Type() != js.TypeFunction {
		return ErrUnsupported
	}
	odc := h.api.Call("getOpenDataContext")
	if !odc.Truthy() || odc.Get("postMessage").Type() != js.TypeFunction {
		return ErrUnsupported
	}
	odc.Call("postMessage", payload)
	return nil
}
