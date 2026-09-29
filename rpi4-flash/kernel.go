package main

const (
	kernelRT     = "7.2.7"
	kernelLatest = "latest"
)

// kernelPlan is what installKernel will do, resolved during preflight so
// that nothing is erased when the requested kernel cannot be provided.
type kernelPlan struct{}

// planKernel: what "7.2.7" and "latest" map to is an open decision, see the
// decision/* branches. Until then the tarball's stock kernel is kept.
func planKernel(r *runner, cfg config) (*kernelPlan, error) {
	r.warn("kernel choice %q not implemented yet, keeping the tarball's kernel", cfg.Kernel)
	return &kernelPlan{}, nil
}

func (f *flasher) installKernel() error {
	f.r.info("keeping the kernel shipped in the tarball")
	return nil
}
