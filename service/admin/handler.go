// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"net/http"

	"github.com/gorilla/rpc/v2"

	apiadmin "github.com/luxfi/api/admin"
	"github.com/luxfi/node/utils/json"
)

// CreateHandler returns an HTTP handler for the admin API.
func (a *Service) CreateHandler() (http.Handler, error) {
	server := rpc.NewServer()
	codec := json.NewCodec()
	server.RegisterCodec(codec, "application/json")
	server.RegisterCodec(codec, "application/json;charset=UTF-8")

	return server, server.RegisterService(&rpcService{svc: a}, "admin")
}

type rpcService struct {
	svc apiadmin.Service
}

func (r *rpcService) StartCPUProfiler(req *http.Request, _ *struct{}, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.StartCPUProfiler(req.Context())
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) StopCPUProfiler(req *http.Request, _ *struct{}, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.StopCPUProfiler(req.Context())
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) MemoryProfile(req *http.Request, _ *struct{}, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.MemoryProfile(req.Context())
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) LockProfile(req *http.Request, _ *struct{}, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.LockProfile(req.Context())
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) Alias(req *http.Request, args *apiadmin.AliasArgs, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.Alias(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) AliasChain(req *http.Request, args *apiadmin.AliasChainArgs, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.AliasChain(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) GetChainAliases(req *http.Request, args *apiadmin.GetChainAliasesArgs, reply *apiadmin.GetChainAliasesReply) error {
	resp, err := r.svc.GetChainAliases(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) Stacktrace(req *http.Request, _ *struct{}, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.Stacktrace(req.Context())
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) SetLoggerLevel(req *http.Request, args *apiadmin.SetLoggerLevelArgs, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.SetLoggerLevel(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) GetLoggerLevel(req *http.Request, args *apiadmin.GetLoggerLevelArgs, reply *apiadmin.LoggerLevelReply) error {
	resp, err := r.svc.GetLoggerLevel(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) GetConfig(req *http.Request, _ *struct{}, reply *any) error {
	resp, err := r.svc.GetConfig(req.Context())
	if err != nil {
		return err
	}
	*reply = resp
	return nil
}

func (r *rpcService) LoadVMs(req *http.Request, _ *struct{}, reply *apiadmin.LoadVMsReply) error {
	resp, err := r.svc.LoadVMs(req.Context())
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) DbGet(req *http.Request, args *apiadmin.DBGetArgs, reply *apiadmin.DBGetReply) error {
	resp, err := r.svc.DbGet(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) ListVMs(req *http.Request, _ *struct{}, reply *apiadmin.ListVMsReply) error {
	resp, err := r.svc.ListVMs(req.Context())
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) SetTrackedChains(req *http.Request, args *apiadmin.SetTrackedChainsArgs, reply *apiadmin.SetTrackedChainsReply) error {
	resp, err := r.svc.SetTrackedChains(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) GetTrackedChains(req *http.Request, _ *struct{}, reply *apiadmin.GetTrackedChainsReply) error {
	resp, err := r.svc.GetTrackedChains(req.Context())
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) Snapshot(req *http.Request, args *apiadmin.SnapshotArgs, reply *apiadmin.SnapshotReply) error {
	resp, err := r.svc.Snapshot(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}

func (r *rpcService) Load(req *http.Request, args *apiadmin.LoadArgs, reply *apiadmin.EmptyReply) error {
	resp, err := r.svc.Load(req.Context(), args)
	if err != nil {
		return err
	}
	if resp != nil {
		*reply = *resp
	}
	return nil
}
