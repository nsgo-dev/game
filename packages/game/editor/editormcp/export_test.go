package editormcp

// Subscribed reports, for the tests, whether a connected session subscribes to the scene and to the selection, as the change stream decides it before encoding them.
func (s *Server) Subscribed() (scene, selection bool) { return s.subscribed() }
