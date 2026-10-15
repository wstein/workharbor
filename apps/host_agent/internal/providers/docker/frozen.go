// SPDX-License-Identifier: EUPL-1.2
package docker

func copyMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return nil
	}
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func copyPolicy(p DriverPolicy) DriverPolicy {
	p.KnownIDs = copyMap(p.KnownIDs)
	p.ImageRefs = copyMap(p.ImageRefs)
	p.ContainerEnv = copyMap(p.ContainerEnv)
	p.RemoteEnv = copyMap(p.RemoteEnv)
	p.StartupTail = append([]string(nil), p.StartupTail...)
	if p.Build != nil {
		b := *p.Build
		b.Args = copyMap(b.Args)
		p.Build = &b
	}
	return p
}
func copyImage(i AdmittedImage) AdmittedImage {
	if i.Document != nil {
		i.Document = clone(i.Document).(map[string]any)
	}
	if i.Config.Volumes != nil {
		i.Config.Volumes = clone(i.Config.Volumes).(map[string]any)
	}
	i.Config.OnBuild = append([]string(nil), i.Config.OnBuild...)
	return i
}
func copyImages(images map[string]AdmittedImage) map[string]AdmittedImage {
	out := make(map[string]AdmittedImage, len(images))
	for k, v := range images {
		out[k] = copyImage(v)
	}
	return out
}
