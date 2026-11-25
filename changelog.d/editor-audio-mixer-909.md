### Added

- `game/editor`: add the Audio Bus Mixer panel (`AudioMixerPanelView`, `AddAudioBus`, `RenameAudioBus`, `DeleteAudioBus`, `MoveAudioBus`, `SetAudioMasterVolume`, `SetAudioBusVolume`, `AddAudioBusEffect`, `SetAudioBusEffect`, `RemoveAudioBusEffect`, `MoveAudioBusEffect`, `AuditionAudioBus`) over `game.yaml`'s `audio` section with undoable `EditProject` persistence, `project.Validate` range enforcement, and live bus auditioning through `project.File.ConfigureMixer` and `sound.Mixer` (#909).
