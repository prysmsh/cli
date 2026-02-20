package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/warp-run/prysm-cli/internal/api"
)

func newMeshRoutesCommand() *cobra.Command {
	routesCmd := &cobra.Command{
		Use:   "routes",
		Short: "Manage DERP mesh exit routes",
	}

	routesCmd.AddCommand(
		newMeshRoutesListCommand(),
		newMeshRoutesCreateCommand(),
		newMeshRoutesDeleteCommand(),
	)

	return routesCmd
}

func newMeshRoutesListCommand() *cobra.Command {
	var clusterRef string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List mesh routes provisioned for your organization",
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

			routes, err := app.API.ListRoutes(ctx, clusterID)
			if err != nil {
				return err
			}

			if len(routes) == 0 {
				color.New(color.FgYellow).Println("No mesh routes defined yet.")
				return nil
			}

			fmt.Printf("%-6s %-16s %-12s %-10s %-14s %-19s\n", "ID", "CLUSTER", "SERVICE", "TARGET", "STATUS", "UPDATED")
			for _, route := range routes {
				clusterName := fmt.Sprintf("%d", route.ClusterID)
				if route.Cluster != nil && strings.TrimSpace(route.Cluster.Name) != "" {
					clusterName = route.Cluster.Name
				}

				service := route.ServiceName
				if route.ServicePort > 0 {
					service = fmt.Sprintf("%s:%d", service, route.ServicePort)
				}

				target := fmt.Sprintf(":%d", route.ExternalPort)
				if route.ExternalURL != "" {
					target = route.ExternalURL
				}

				updated := route.UpdatedAt.Format(time.RFC3339)
				fmt.Printf("%-6d %-16s %-12s %-10s %-14s %-19s\n",
					route.ID,
					clusterName,
					service,
					target,
					route.Status,
					updated,
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterRef, "cluster", "", "filter routes by cluster name or ID")
	return cmd
}

func newMeshRoutesCreateCommand() *cobra.Command {
	var (
		clusterRef   string
		routeName    string
		description  string
		serviceName  string
		servicePort  int
		externalPort int
		protocol     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new mesh exit route via DERP",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(clusterRef) == "" {
				return errors.New("cluster reference is required (--cluster)")
			}
			if strings.TrimSpace(serviceName) == "" {
				return errors.New("service name is required (--service)")
			}
			if servicePort <= 0 || servicePort > 65535 {
				return errors.New("service port must be between 1-65535")
			}

			protocol = strings.ToUpper(strings.TrimSpace(protocol))
			if protocol == "" {
				protocol = "TCP"
			}
			if protocol != "TCP" && protocol != "UDP" {
				return errors.New("protocol must be TCP or UDP")
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			cluster, err := resolveCluster(ctx, app, clusterRef)
			if err != nil {
				return err
			}

			extPort := externalPort
			if extPort == 0 {
				suggested, err := app.API.SuggestRoutePort(ctx, &cluster.ID)
				if err != nil {
					return fmt.Errorf("suggest external port: %w", err)
				}
				extPort = suggested
			}

			req := api.RouteCreateRequest{
				Name:         routeName,
				Description:  description,
				ClusterID:    cluster.ID,
				ServiceName:  serviceName,
				ServicePort:  servicePort,
				ExternalPort: extPort,
				Protocol:     protocol,
			}

			route, err := app.API.CreateRoute(ctx, req)
			if err != nil {
				return err
			}

			color.New(color.FgGreen).Printf("🛣️  Route %d created targeting %s\n", route.ID, cluster.Name)
			fmt.Printf("Local clients can reach %s via %s (%s).\n",
				serviceEndpointLabel(route.ServiceName, route.ServicePort),
				displayRouteEndpoint(route.ExternalURL, route.ExternalPort),
				route.Protocol,
			)
			if route.Description != "" {
				color.New(color.FgHiBlack).Printf("Notes: %s\n", route.Description)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&clusterRef, "cluster", "", "exit-enabled cluster name or ID")
	cmd.Flags().StringVar(&routeName, "name", "", "human-friendly route name")
	cmd.Flags().StringVar(&description, "description", "", "optional route description")
	cmd.Flags().StringVar(&serviceName, "service", "", "target service name or mesh hostname")
	cmd.Flags().IntVar(&servicePort, "service-port", 0, "target service port inside the cluster")
	cmd.Flags().IntVar(&externalPort, "external-port", 0, "external DERP port to allocate (auto if omitted)")
	cmd.Flags().StringVar(&protocol, "protocol", "tcp", "route protocol (tcp|udp)")

	_ = cmd.MarkFlagRequired("cluster")
	_ = cmd.MarkFlagRequired("service")
	_ = cmd.MarkFlagRequired("service-port")

	return cmd
}

func newMeshRoutesDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete <route-id>",
		Aliases: []string{"rm"},
		Short:   "Delete an existing mesh route",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			routeID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid route id: %w", err)
			}

			app := MustApp()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			if err := app.API.DeleteRoute(ctx, routeID); err != nil {
				return err
			}

			color.New(color.FgGreen).Printf("🗑️  Route %d deleted\n", routeID)
			return nil
		},
	}

	return cmd
}

func resolveCluster(ctx context.Context, app *App, ref string) (*api.Cluster, error) {
	clusters, err := app.API.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	if len(clusters) == 0 {
		return nil, errors.New("no clusters available")
	}

	cluster, err := findCluster(clusters, ref)
	if err != nil {
		var b strings.Builder
		fmt.Fprintf(&b, "%v\nAvailable clusters:\n", err)
		for _, c := range clusters {
			status := c.Status
			if strings.ToLower(status) == "connected" {
				status = color.HiGreenString(status)
			} else {
				status = color.HiRedString(status)
			}
			fmt.Fprintf(&b, "  - %d\t%s\t%s\n", c.ID, c.Name, status)
		}
		return nil, errors.New(b.String())
	}

	if !cluster.IsExitRouter {
		color.New(color.FgYellow).Printf("Warning: cluster %s is not currently marked as an exit router.\n", cluster.Name)
	}

	return cluster, nil
}

func serviceEndpointLabel(name string, port int) string {
	if port > 0 {
		return fmt.Sprintf("%s:%d", name, port)
	}
	return name
}

func displayRouteEndpoint(url string, port int) string {
	if strings.TrimSpace(url) != "" {
		return url
	}
	return fmt.Sprintf(":%d", port)
}
