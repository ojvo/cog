package main

import (
	"fmt"
	"c.n/ojv/cog/cfg"
	"c.n/ojv/cog/cor"
	"os"
	"strings"
)

func main() {
	const configFile = "config.cfg"

	// Check if config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Printf("Config file %s not found. Please ensure it exists in the current directory.\n", configFile)
		return
	}

	// [New] Use cor.Init for unified initialization
	if err := cor.Init(configFile); err != nil {
		fmt.Printf("Error initializing core: %v\n", err)
		return
	}
	defer cor.Close()

	// [New] Validate the configuration against a schema
	schema := cfg.Schema{
		"app.name":    {Type: "string", Required: true},
		"server.port": {Type: "int", Required: true},
		"database":    {Type: "section", Required: true},
		"services":    {Type: "slice", Required: false},
	}

	conf := cor.ConfigInstance()
	if err := conf.Validate(schema); err != nil {
		cor.Errorf("Config Validation Failed: %v", err)
		return
	}

	cor.Info("=== Cog Unified Module Demo ===")
	cor.Infof("Loaded from: %s", configFile)

	// 1. Global Settings (Using shorthand getters)
	cor.Info("[1. Global Settings]")
	cor.Infof("App Name:    %s", cor.GetString("app.name"))
	cor.Infof("Version:     %s", cor.GetString("app.version"))
	cor.Infof("Environment: %s", cor.GetString("app.env"))

	// 2. Server Scope (Braces)
	cor.Info("[2. Server Configuration (Braces)]")
	cor.Infof("Address:     %s:%d", cor.GetString("server.host"), cor.GetInt("server.port"))
	cor.Infof("SSL Enabled: %v", cor.GetBool("server.ssl.enabled"))
	cor.Infof("Read Timeout: %v", cor.GetString("server.timeout.read"))

	// 3. Database (Traditional Sections)
	cor.Info("[3. Database (Traditional Sections)]")
	cor.Infof("Driver:      %s", cor.GetString("database.driver"))
	cor.Infof("Pool Max:    %d", cor.GetInt("database.pool.max_open"))
	cor.Infof("Password:    %s (from env or default)", cor.GetString("database.password"))

	// 4. Multiline String
	cor.Info("[4. Multiline Message]")
	motd := cor.GetString("app.motd")
	// Indent the MOTD for display
	fmt.Println(strings.ReplaceAll(motd, "\n", "\n    "))
	fmt.Println()

	// 5. Services (Slices)
	cor.Info("[5. Services List (Slices)]")
	if services, ok := cor.GetSlice("services"); ok {
		for i, svc := range services {
			cor.Infof("Service #%d:", i+1)
			cor.Infof("  Name:     %s", svc.GetString("name"))
			cor.Infof("  Endpoint: %s", svc.GetString("endpoint"))
			cor.Infof("  Tags:     %s", svc.GetString("tags"))
		}
	} else {
		cor.Warn("No services found in config")
	}

	// 6. Manual Binding
	cor.Info("[6. Zero-Reflection Manual Binding]")
	type ServerInfo struct {
		Host string
		Port int
	}
	var si ServerInfo
	conf.Bind("server.host", &si.Host)
	conf.Bind("server.port", &si.Port)
	cor.Infof("Bound ServerInfo: %+v", si)

	cor.Info("==============================================")
}
