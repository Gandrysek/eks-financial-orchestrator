//go:build tools
// +build tools

// Package main provides tool dependency tracking for the EKS Financial Orchestrator.
// This file ensures that tool and future dependencies are tracked in go.mod
// even before they are imported in production code.
package main

import (
	_ "github.com/aws/aws-sdk-go-v2"
	_ "github.com/aws/aws-sdk-go-v2/service/costexplorer"
	_ "github.com/aws/aws-sdk-go-v2/service/ec2"
	_ "github.com/aws/aws-sdk-go-v2/service/s3"
	_ "github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/prometheus/client_golang/prometheus"
	_ "pgregory.net/rapid"
)
