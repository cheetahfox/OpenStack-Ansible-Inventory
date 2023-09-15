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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"gopkg.in/yaml.v2"
)

type vms struct {
	UUID        string
	Name        string
	ProjectID   string
	IpAddresses OsAddresses
	Status      string
}

type OsAddresses []struct {
	OSEXTIPSMACMacAddr string `json:"OS-EXT-IPS-MAC:mac_addr"`
	OSEXTIPSType       string `json:"OS-EXT-IPS:type"`
	Addr               string `json:"addr"`
	Version            int    `json:"version"`
}

type AllHosts struct {
	All Hosts `yaml:"all"`
}

type Hosts struct {
	Hosts map[string]Ansiblehost `yaml:"hosts"`
	Var   Ansiblevars            `yaml:"vars"`
}

type Ansiblehost struct {
	HostIp   string `yaml:"ansible_host"`
	Hostname string `yaml:"hostname"`
}

// AnsibleVars is the struct for the ansible vars
type Ansiblevars struct {
	Ansibleuser          string `yaml:"ansible_user"`
	Ansiblesshcommonargs string `yaml:"ansible_ssh_common_args"`
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

// populateServers will populate the vms struct with the current running instances
// in the OpenStack project.
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

		for _, addresses := range server.Addresses {
			jsonStr, err := json.Marshal(addresses)
			if err != nil {
				fmt.Println(err)
			}

			var osaddr OsAddresses
			if err := json.Unmarshal([]byte(jsonStr), &osaddr); err != nil {
				fmt.Println(err)
			}

			s.IpAddresses = osaddr
		}

		osServers = append(osServers, s)
	}

	/*
		found := fmt.Sprintf("Found %d OpenStack instances", len(osServers))
		fmt.Println(found)
	*/
	return osServers, nil
}

// Get an address from the nodes. If we don't get one error out.
func extractIP(ip OsAddresses) (string, error) {
	var addr string
	for _, v := range ip {
		addr = v.Addr
	}

	if addr == "" {
		err := errors.New("unable to find any address")
		return addr, err
	}

	return addr, nil
}

// populateHosts will populate the AllHosts struct with the current running instances
func populateHosts(vms []vms) AllHosts {
	var allHosts AllHosts

	allHosts.All.Hosts = make(map[string]Ansiblehost)

	for _, s := range vms {
		if s.Status == "ACTIVE" {
			ip, err := extractIP(s.IpAddresses)
			if err != nil {
				panic(err)
			}
			var ansibleHost Ansiblehost
			ansibleHost.HostIp = ip
			ansibleHost.Hostname = s.Name
			allHosts.All.Hosts[s.Name] = ansibleHost
		}
	}

	return allHosts
}

// populateVars will populate the AllHosts struct with hardcoded vars for now
// TODO: Make this dynamic
func populateVars(inventory AllHosts) (AllHosts, error) {
	inventory.All.Var.Ansibleuser = "ubuntu"
	inventory.All.Var.Ansiblesshcommonargs = "-o StrictHostKeyChecking=no"

	return inventory, nil
}

/*
This function will create a script that will reset the ssh keys on the nodes.
This is useful if you are using a cloud-init script that sets the ssh keys
on the nodes. This will reset the ssh keys on the nodes so that you can ssh
into them with the new keys.

Format of the script is as follows:

#!/bin/bash
ssh-keygen -f $HOME/.ssh/known_hosts -R "IP_ADDRESS"
ssh-keygen -f $HOME/.ssh/known_hosts -R "HOSTNAME"

*/
func createSSHResetScript(vms AllHosts, domain string, projectname string) {
	var script string
	script = "#!/bin/bash\n"

	for _, v := range vms.All.Hosts {
		script = script + "ssh-keygen -f $HOME/.ssh/known_hosts -R " + v.HostIp + "\n"
		if domain == "" {
			script = script + "ssh-keygen -f $HOME/.ssh/known_hosts -R " + v.Hostname + "\n"
		} else {
			script = script + "ssh-keygen -f $HOME/.ssh/known_hosts -R " + v.Hostname + "." + domain + "\n"
		}
	}

	err := os.WriteFile("reset-ssh-"+projectname+".sh", []byte(script), 0755)
	if err != nil {
		panic(err)
	}
	fmt.Println("Wrote reset-ssh-" + projectname + ".sh")
}

func main() {
	osProvder := startup()

	vms, err := populateServers(osProvder)
	if err != nil {
		panic(err)
	}

	yamlinventory := populateHosts(vms)
	yamlinventory, err = populateVars(yamlinventory)
	if err != nil {
		panic(err)
	}

	yamlout, err := yaml.Marshal(&yamlinventory)
	if err != nil {
		panic(err)
	}

	// This name is already verifyed to be set in startup()
	projectname := os.Getenv("OS_PROJECT_NAME")
	filename := projectname + ".yaml"

	err = os.WriteFile(filename, yamlout, 0644)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Wrote %s\n", filename)

	if os.Getenv("DEBUG") != "" {
		fmt.Println(string(yamlout))
	}

	if os.Getenv("SSH_RESET") == "true" {
		fmt.Println("Also Generating SSH Reset Script")
		createSSHResetScript(yamlinventory, os.Getenv("DNS_DOMAIN"), projectname)
	}

}
