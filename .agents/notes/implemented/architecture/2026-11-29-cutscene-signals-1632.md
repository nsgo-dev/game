# Cutscene signal markers

Signal tracks add deterministic authored event markers to timelines. `TimelineTarget.OnSignal` fires only while stepping across a marker, while seeking remains side-effect free.
