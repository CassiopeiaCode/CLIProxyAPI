package executor

import cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"

func originalRequestBytes(opts cliproxyexecutor.Options) []byte {
	return opts.OriginalRequestBytes()
}

func originalRequestOr(opts cliproxyexecutor.Options, fallback []byte) []byte {
	return opts.OriginalRequestOr(fallback)
}
