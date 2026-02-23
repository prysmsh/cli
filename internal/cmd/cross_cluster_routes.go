package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prysmsh/cli/internal/api"
	"github.com/prysmsh/cli/internal/style"
	"github.com/prysmsh/cli/internal/ui"
)

func newCrossClusterRoutesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cross-cluster-routes",
		Aliases: []string{"ccr"},
		Short:   "Manage cross-cluster routes via DERP relay",
	}

	cmd.AddCommand(
		newCCRListCommand(),
		newCCRCreateCommand(),
		newCCRDeleteCommand(),
		newCCRToggleCommand(),
	)

	return cmd
}

func newCCRListCommand() *cobra.Command {
	var clusterRef string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cross-cluster routes for your organization",
		RunE: func(cmd *cobra.Command, args []string) error {
			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			var clusterID *int64
			if strings.TrimSpace(clusterRef) != "" {
				cluster, err := resolveCluster(ctx, app, clusterRef)
				if err != nil {
					return err
				}
				clusterID = &cluster.ID
			}

			routes, err := app.API.ListCrossClusterRoutes(ctx, clusterID)
			if err != nil {
				return err
			}

			if len(routes) == 0 {
				fmt.Println(style.Warning.Render("No cross-cluster routes defined yet."))
				return nil
			}

			headers := []string{"ID", "NAME", "SOURCE", "TARGET", "SERVICE", "PORT", "STATUS"}
			rows := make([][]string, 0, len(routes))
			for _, r := range routes {
				src := fmt.Sprintf("%d", r.SourceClusterID)
				if r.SourceCluster != nil && r.SourceCluster.Name != "" {
					src = r.SourceCluster.Name
				}
				tgt := fmt.Sprintf("%d", r.TargetClusterID)
				if r.TargetCluster != nil && r.TargetCluster.Name != "" {
					tgt = r.TargetCluster.Name
				}
				svc := r.TargetService
				if r.TargetNamespace != "" && r.TargetNamespace != "default" {
					svc = r.TargetNamespace + "/" + svc
				}
				rows = append(rows, []string{
					fmt.Sprintf("%d", r.ID),
					r.Name,
					src,
					tgt,
					svc,
					fmt.Sprintf("%d", r.LocalPort),
					r.Status,
				})
			}
			ui.PrintTable(headers, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterRef, "cluster", "", "filter by cluster name or ID")
	return cmd
}

func newCCRCreateCommand() *cobra.Command {
	var (
		name            string
		sourceRef       string
		targetRef       string
		targetService   string
		targetNamespace string
		targetPort      int
		localPort       int
		protocol        string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new cross-cluster route",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("route name is required (--name)")
			}
			if strings.TrimSpace(sourceRef) == "" {
				return errors.New("source cluster is required (--source)")
			}
			if strings.TrimSpace(targetRef) == "" {
				return errors.New("target cluster is required (--target)")
			}
			if strings.TrimSpace(targetService) == "" {
				return errors.New("target service is required (--service)")
			}
			if targetPort <= 0 || targetPort > 65535 {
				return errors.New("target port must be between 1-65535 (--target-port)")
			}
			if localPort <= 0 || localPort > 65535 {
				return errors.New("local port must be between 1-65535 (--local-port)")
			}

			protocol = strings.ToLower(strings.TrimSpace(protocol))
			if protocol == "" {
				protocol = "tcp"
			}
			if protocol != "tcp" && protocol != "udp" {
				return errors.New("protocol must be tcp or udp")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			source, err := resolveCluster(ctx, app, sourceRef)
			if err != nil {
				return fmt.Errorf("resolve source cluster: %w", err)
			}

			target, err := resolveCluster(ctx, app, targetRef)
			if err != nil {
				return fmt.Errorf("resolve target cluster: %w", err)
			}

			if source.ID == target.ID {
				return errors.New("source and target clusters must be different")
			}

			req := api.CrossClusterRouteCreateRequest{
				Name:            name,
				SourceClusterID: source.ID,
				TargetClusterID: target.ID,
				TargetService:   targetService,
				TargetNamespace: targetNamespace,
				TargetPort:      targetPort,
				LocalPort:       localPort,
				Protocol:        protocol,
			}

			route, err := app.API.CreateCrossClusterRoute(ctx, req)
			if err != nil {
				return err
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Cross-cluster route %d created: %s -> %s",
				route.ID, source.Name, target.Name)))
			fmt.Printf("  Service: %s:%d  Local port: %d  Protocol: %s\n",
				route.TargetService, route.TargetPort, route.LocalPort, route.Protocol)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "route name")
	cmd.Flags().StringVar(&sourceRef, "source", "", "source cluster name or ID")
	cmd.Flags().StringVar(&targetRef, "target", "", "target cluster name or ID")
	cmd.Flags().StringVar(&targetService, "service", "", "target service name")
	cmd.Flags().StringVar(&targetNamespace, "namespace", "", "target namespace (default: 'default')")
	cmd.Flags().IntVar(&targetPort, "target-port", 0, "target service port (1-65535)")
	cmd.Flags().IntVar(&localPort, "local-port", 0, "local listener port on source cluster (1-65535)")
	cmd.Flags().StringVar(&protocol, "protocol", "tcp", "route protocol (tcp|udp)")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("target")
	_ = cmd.MarkFlagRequired("service")
	_ = cmd.MarkFlagRequired("target-port")
	_ = cmd.MarkFlagRequired("local-port")

	return cmd
}

func newCCRDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <route-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a cross-cluster route",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			routeID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid route id: %w", err)
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.DeleteCrossClusterRoute(ctx, routeID); err != nil {
				return err
			}

			fmt.Println(style.Success.Render(fmt.Sprintf("Cross-cluster route %d deleted", routeID)))
			return nil
		},
	}
}

func newCCRToggleCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "toggle <route-id>",
		Short: "Enable or disable a cross-cluster route",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			routeID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid route id: %w", err)
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			route, err := app.API.ToggleCrossClusterRoute(ctx, routeID)
			if err != nil {
				return err
			}

			state := "disabled"
			if route.Enabled {
				state = "enabled"
			}
			fmt.Println(style.Success.Render(fmt.Sprintf("Cross-cluster route %d %s", route.ID, state)))
			return nil
		},
	}
}

