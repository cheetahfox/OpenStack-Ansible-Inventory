/*
Author  : Joshua Snyder
Program : OpenStack-Ansible-Inventory
Date    : 05-17-2023

This program connects to an OpenStack Cloud and generates a Ansible Inventory file
in Yaml format from the currently running OpenStack instances.

In the past I have seen several python scrips that claimed to be able to generate
Ansible inventory files from a running cluster. While they like might have worked
in the past, I have not been able to make them work in my clusters and deployments.

So I am taking parts of my OpenStack instance metrics program and I am going to use
it to generate these Ansible Inventory files. All bugs and horrible code our mine.
*/

package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
)

type vms struct {
	UUID      string
	Name      string
	ProjectID string
	IP        string
	Status    string
}

func startup() *gophercloud.ProviderClient {

	// Required Enviorment vars mostly OpenStack Env vars.
	requiredEnvVars := []string{
		"OS_AUTH_URL",
		"OS_USERNAME",
		"OS_PASSWORD",
		"OS_PROJECT_DOMAIN_ID",
		"OS_REGION_NAME",
		"OS_PROJECT_NAME",
		"OS_USER_DOMAIN_NAME",
		"OS_INTERFACE",
		"OS_PROJECT_ID",
		"OS_DOMAIN_NAME",
		"OS_REGION_NAME",
	}

	// Newer Openstack Env might not have this set, so if we have USER domain we match it
	if os.Getenv("OS_DOMAIN_NAME") == "" || os.Getenv("OS_USER_DOMAIN_NAME") != "" {
		os.Setenv("OS_DOMAIN_NAME", os.Getenv("OS_USER_DOMAIN_NAME"))
	}

	// Check if the Required Enviromental varibles are set exit if they aren't.
	for index := range requiredEnvVars {
		if os.Getenv(requiredEnvVars[index]) == "" {
			log.Fatalf("Missing %s Enviroment var \n", requiredEnvVars[index])
		}
	}

	provider, err := osAuth()
	if err != nil {
		fmt.Println("Error while Authenticating with OpenStack for the first time.")
		log.Fatal(err)
	}

	return provider
}

/*
Authenticate using the Enviromental vars
Return ProviderClient and err
*/
func osAuth() (*gophercloud.ProviderClient, error) {
	// Lets connect to Openstack now using these values
	opts, err := openstack.AuthOptionsFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	// This is super important, because the token will expire.
	opts.AllowReauth = true

	provider, err := openstack.AuthenticatedClient(opts)
	if err != nil {
		log.Fatal(err)
	}

	r := provider.GetAuthResult()
	if r == nil {
		return nil, errors.New("no valid auth result")
	}
	return provider, err
}

func populateServers(provider *gophercloud.ProviderClient) ([]vms, error) {
	var osServers []vms

	endpoint := gophercloud.EndpointOpts{Region: os.Getenv("OS_REGION_NAME")}
	client, err := openstack.NewComputeV2(provider, endpoint)
	if err != nil {
		return nil, err
	}

	listOpts := servers.ListOpts{
		AllTenants: false,
		Name:       "",
	}
	/* If we are doing a site wide scan
	if config.Scope == "site" {
		listOpts.AllTenants = true
	}
	*/

	allPages, err := servers.List(client, listOpts).AllPages()
	if err != nil {
		return nil, err
	}
	allServers, err := servers.ExtractServers(allPages)
	if err != nil {
		return nil, err
	}

	var s vms

	for _, server := range allServers {
		s.UUID = server.ID
		s.Name = server.Name
		s.ProjectID = server.TenantID
		s.Status = server.Status
		s.IP = server.AccessIPv4
		osServers = append(osServers, s)
	}

	/*
		found := fmt.Sprintf("Found %d OpenStack instances", len(osServers))
		fmt.Println(found)
	*/
	return osServers, nil
}

func main() {
	osProvder := startup()

	vms, err := populateServers(osProvder)
	if err != nil {
		panic(err)
	}

	// debug output while getting everything setup before doing the yaml output
	for _, s := range vms {
		if s.Status == "ACTIVE" {
			fmt.Print(s.Name + " :")
			fmt.Println(s.IP)
		}
	}

}
