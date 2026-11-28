# Motion behaviors

`pkg/motion` provides deterministic helpers for scene behavior components. `Bullet`, `Sine`, and `Rotate` advance only from the supplied fixed-step duration, so simulating one second at different fixed rates gives the same result within floating-point tolerance. `Sine` supports sine and triangle waves, while `Rotate` wraps degrees to `[0, 360)`.
