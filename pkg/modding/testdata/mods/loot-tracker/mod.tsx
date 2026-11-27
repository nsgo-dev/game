import { onEvent, emit, getState, log } from "game";
import { countItem, listItems } from "mygame/inventory";

onEvent("loot_picked_up", (payload) => {
  const item = payload?.item ?? "gem";
  const total = countItem(item);
  log("Picked up", item, "total:", total, "zone:", getState("zone"));
  emit("loot-tracker:updated", { item, total });
});

export function summarize(): { items: string[]; gems: number } {
  return {
    items: listItems(),
    gems: countItem("gem"),
  };
}
