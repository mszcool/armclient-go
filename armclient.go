package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/dgrijalva/jwt-go"
	"github.com/urfave/cli"
)

const (
	appVersion   = "0.2.3"
	userAgentStr = "github.com/yangl900/armclient-go"
	flagVerbose  = "verbose"
	flagRaw      = "raw, r"
	flagTenantID = "tenant, t"
	flagHeader   = "header, H"
)

func main() {
	app := cli.NewApp()

	app.Name = "armclient"
	app.Usage = "Command line client for Azure Resource Manager APIs."
	app.Version = appVersion
	app.Description = `
		Cross-platform open source command line tool for accessing Azure Resource Manager REST APIs.
		Github repo: https://github.com/yangl900/armclient-go

		This is a Go implementation of original windows version ARMClient (https://github.com/projectkudu/ARMClient/).
		Commands are kept same as much as possible, and now you can enjoy the useful tool on Linux & Mac.
		Additionally in Azure Cloud Shell (https://shell.azure.com/), login is handled automatically. It just works.`

	app.Action = func(c *cli.Context) error {
		cli.ShowAppHelp(c)
		return nil
	}

	log.SetOutput(ioutil.Discard)

	verboseFlag := cli.BoolFlag{
		Name:  flagVerbose,
		Usage: "Output verbose messages like request Uri, headers etc.",
	}

	headerFlag := cli.StringSliceFlag{
		Name:  flagHeader,
		Usage: "Specify additional request headers.",
	}

	rawFlag := cli.BoolFlag{
		Name:  flagRaw,
		Usage: "Print out raw acces token.",
	}

	tenantIDFlag := cli.StringFlag{
		Name:  flagTenantID,
		Usage: "Specify the tenant Id.",
	}

	app.Flags = []cli.Flag{verboseFlag}

	app.Commands = []cli.Command{
		{
			Name:   "get",
			Action: doRequest,
			Usage:  "Makes a GET request to ARM endpoint.",
			Flags:  []cli.Flag{verboseFlag, headerFlag},
		},
		{
			Name:   "head",
			Action: doRequest,
			Usage:  "Makes a HEAD request to ARM endpoint.",
			Flags:  []cli.Flag{verboseFlag, headerFlag},
		},
		{
			Name:   "put",
			Action: doRequest,
			Usage:  "Makes a PUT request to ARM endpoint.",
			Flags:  []cli.Flag{verboseFlag, headerFlag},
		},
		{
			Name:   "patch",
			Action: doRequest,
			Usage:  "Makes a PATCH request to ARM endpoint.",
			Flags:  []cli.Flag{verboseFlag, headerFlag},
		},
		{
			Name:   "delete",
			Action: doRequest,
			Usage:  "Makes a DELETE request to ARM endpoint.",
			Flags:  []cli.Flag{verboseFlag, headerFlag},
		},
		{
			Name:   "post",
			Action: doRequest,
			Usage:  "Makes a POST request to ARM endpoint.",
			Flags:  []cli.Flag{verboseFlag, headerFlag},
		},
		{
			Name:   "token",
			Action: printToken,
			Usage:  "Prints the specified tenant access token. If not specified, default to current tenant.",
			Flags:  []cli.Flag{rawFlag, tenantIDFlag},
		},
		{
			Name:   "tenant",
			Action: printTenants,
			Usage:  "Manage tenants (Azure AD directory) current account have access to. Set / show active tenant.",
			Subcommands: []cli.Command{
				{
					Name:   "set",
					Action: setActiveTenant,
					Usage:  "Sets an active tenant.",
				},
				{
					Name:   "show",
					Action: showActiveTenant,
					Usage:  "Shows current active tenant.",
				},
				{
					Name:   "list",
					Action: printTenants,
					Usage:  "Shows all tenants.",
				},
			},
		},
	}

	app.CustomAppHelpTemplate = cli.AppHelpTemplate

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
	}
}

func isWriteVerb(verb string) bool {
	v := strings.ToUpper(verb)
	return v == "PUT" || v == "POST" || v == "PATCH"
}

func doRequest(c *cli.Context) error {
	if len(c.Args()) == 0 {
		return errors.New("No path specified")
	}

	url, err := getRequestURL(c.Args().First())
	if err != nil {
		return err
	}

	var reqBody string
	if isWriteVerb(c.Command.Name) && c.NArg() > 1 {
		reqBody = c.Args().Get(1)

		if strings.HasPrefix(reqBody, "@") {
			filePath, _ := filepath.Abs(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(reqBody, "@"), "'"), "'"))

			if _, err := os.Stat(filePath); err != nil {
				return errors.New("File not found: " + filePath)
			}

			buffer, err := ioutil.ReadFile(filePath)
			if err != nil {
				return errors.New("Failed to read file: " + filePath)
			}

			reqBody = prettyJSON(buffer)
		} else {
			reqBody = prettyJSON([]byte(reqBody))
			fmt.Println(reqBody)
		}
	}

	client := &http.Client{}
	req, _ := http.NewRequest(strings.ToUpper(c.Command.Name), url, bytes.NewReader([]byte(reqBody)))

	token, err := acquireAuthTokenCurrentTenant()
	if err != nil {
		return errors.New("Failed to acquire auth token: " + err.Error())
	}

	req.Header.Set("Authorization", token)
	req.Header.Set("User-Agent", userAgentStr)
	req.Header.Set("x-ms-client-request-id", newUUID())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	additionalHeaders := c.StringSlice(strings.Split(flagHeader, ",")[0])
	headerNames := make([]string, 0)
	if additionalHeaders != nil {
		for _, h := range additionalHeaders {
			segments := strings.Split(h, "=")
			if len(segments) == 2 {
				req.Header.Set(segments[0], segments[1])
				headerNames = append(headerNames, segments[0])
			} else {
				return fmt.Errorf("Cannot parse specified header '%s'. Value must be in format Header=Value", h)
			}
		}
	}

	start := time.Now()
	response, err := client.Do(req)
	if err != nil {
		return errors.New("Request failed: " + err.Error())
	}

	defer response.Body.Close()
	buf, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return errors.New("Request failed: " + err.Error())
	}

	if c.GlobalBool(flagVerbose) || c.Bool(flagVerbose) {
		fmt.Println(responseDetail(response, time.Now().Sub(start), headerNames))
	}

	fmt.Println(prettyJSON(buf))
	return nil
}

func printToken(c *cli.Context) error {
	tenantID := c.String(strings.Split(flagTenantID, ",")[0])

	var token string
	var err error
	if tenantID == "" {
		token, err = acquireAuthTokenCurrentTenant()
	} else {
		token, err = acquireAuthToken(tenantID)
	}

	if err != nil {
		return errors.New("Failed to get access token: " + err.Error())
	}

	if c.Bool(strings.Split(flagRaw, ",")[0]) {
		fmt.Println(token)
		clipboard.WriteAll(token)
	} else {
		segments := strings.Split(token, ".")

		if len(segments) != 3 {
			return errors.New("Invalid JWT token retrieved")
		}

		decoded, _ := jwt.DecodeSegment(segments[1])

		fmt.Println(prettyJSON(decoded))

		if !clipboard.Unsupported {
			err := clipboard.WriteAll(token)
			if err == nil {
				fmt.Println("\n\nToken copied to clipboard successfully.")
			}
		}
	}

	return nil
}

func printTenants(c *cli.Context) error {
	token, err := acquireAuthTokenCurrentTenant()
	if err != nil {
		return errors.New("Failed to get access token: " + err.Error())
	}

	tenants, err := getTenants(token)
	if err != nil {
		return errors.New("Failed to get tenants: " + err.Error())
	}

	buffer, _ := json.Marshal(tenants)
	fmt.Println(prettyJSON(buffer))

	return nil
}

func setActiveTenant(c *cli.Context) error {
	if c.NArg() == 0 {
		return errors.New("No tenant Id specified")
	}

	specifiedTenant := c.Args().First()

	token, err := acquireAuthTokenCurrentTenant()
	if err != nil {
		return errors.New("Failed to get access token: " + err.Error())
	}

	tenants, err := getTenants(token)
	if err != nil {
		return errors.New("Failed to get tenants: " + err.Error())
	}

	for _, t := range tenants {
		if strings.ToLower(t.TenantID) == strings.ToLower(specifiedTenant) {
			saveSettings(settings{ActiveTenant: specifiedTenant})
			return nil
		}
	}

	return fmt.Errorf("You don't have access to specified tenant: %s", specifiedTenant)
}

func showActiveTenant(c *cli.Context) error {
	_, err := acquireAuthTokenCurrentTenant()
	if err != nil {
		return errors.New("Failed to get access token: " + err.Error())
	}

	setting, err := readSettings()
	if err != nil {
		return fmt.Errorf("Failed to show current tenant: %v", err)
	}

	fmt.Println(setting.ActiveTenant)

	return nil
}
