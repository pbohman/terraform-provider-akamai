package appsec

import (
	"encoding/json"
	"testing"

	"github.com/akamai/AkamaiOPEN-edgegrid-golang/v3/pkg/appsec"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAkamaiAPICoProtection_res_basic(t *testing.T) {
	t.Run("match by APIConstraintsProtection ID", func(t *testing.T) {
		client := &appsec.Mock{}

		config := appsec.GetConfigurationResponse{}
		err := json.Unmarshal(loadFixtureBytes("testdata/TestResConfiguration/LatestConfiguration.json"), &config)
		require.NoError(t, err)

		updateResponseAllProtectionsFalse := appsec.UpdateAPIConstraintsProtectionResponse{}
		err = json.Unmarshal(loadFixtureBytes("testdata/TestResAPIConstraintsProtection/PolicyProtections.json"), &updateResponseAllProtectionsFalse)
		require.NoError(t, err)

		getResponseAllProtectionsFalse := appsec.GetAPIConstraintsProtectionResponse{}
		err = json.Unmarshal(loadFixtureBytes("testdata/TestResAPIConstraintsProtection/PolicyProtections.json"), &getResponseAllProtectionsFalse)
		require.NoError(t, err)

		updateResponseOneProtectionTrue := appsec.UpdateAPIConstraintsProtectionResponse{}
		err = json.Unmarshal(loadFixtureBytes("testdata/TestResAPIConstraintsProtection/UpdatedPolicyProtections.json"), &updateResponseOneProtectionTrue)
		require.NoError(t, err)

		getResponseOneProtectionTrue := appsec.GetAPIConstraintsProtectionResponse{}
		err = json.Unmarshal(loadFixtureBytes("testdata/TestResAPIConstraintsProtection/UpdatedPolicyProtections.json"), &getResponseOneProtectionTrue)
		require.NoError(t, err)

		// Mock each call to the EdgeGrid library. With the exception of GetConfiguration, each call
		// is mocked individually because calls with the same parameters may have different return values.

		// All calls to GetConfiguration have same parameters and return value
		client.On("GetConfiguration",
			mock.Anything,
			appsec.GetConfigurationRequest{ConfigID: 43253},
		).Return(&config, nil)

		// Create, with terminal Read
		client.On("UpdateAPIConstraintsProtection",
			mock.Anything,
			appsec.UpdateAPIConstraintsProtectionRequest{ConfigID: 43253, Version: 7, PolicyID: "AAAA_81230"},
		).Return(&updateResponseAllProtectionsFalse, nil).Once()
		client.On("GetAPIConstraintsProtection",
			mock.Anything,
			appsec.GetAPIConstraintsProtectionRequest{ConfigID: 43253, Version: 7, PolicyID: "AAAA_81230"},
		).Return(&getResponseAllProtectionsFalse, nil).Once()

		// Reads performed via "id" and "enabled" checks
		client.On("GetAPIConstraintsProtection",
			mock.Anything,
			appsec.GetAPIConstraintsProtectionRequest{ConfigID: 43253, Version: 7, PolicyID: "AAAA_81230"},
		).Return(&getResponseAllProtectionsFalse, nil).Once()
		client.On("GetAPIConstraintsProtection",
			mock.Anything,
			appsec.GetAPIConstraintsProtectionRequest{ConfigID: 43253, Version: 7, PolicyID: "AAAA_81230"},
		).Return(&getResponseAllProtectionsFalse, nil).Once()

		// Update, with terminal Read
		client.On("UpdateAPIConstraintsProtection",
			mock.Anything,
			appsec.UpdateAPIConstraintsProtectionRequest{ConfigID: 43253, Version: 7, PolicyID: "AAAA_81230",
				ApplyAPIConstraints: true},
		).Return(&updateResponseOneProtectionTrue, nil).Once()
		client.On("GetAPIConstraintsProtection",
			mock.Anything,
			appsec.GetAPIConstraintsProtectionRequest{ConfigID: 43253, Version: 7, PolicyID: "AAAA_81230"},
		).Return(&getResponseOneProtectionTrue, nil).Once()

		// Read, performed as part of "id" check.
		client.On("GetAPIConstraintsProtection",
			mock.Anything,
			appsec.GetAPIConstraintsProtectionRequest{ConfigID: 43253, Version: 7, PolicyID: "AAAA_81230"},
		).Return(&getResponseOneProtectionTrue, nil).Once()

		// Delete, performed automatically to clean up
		client.On("UpdateAPIConstraintsProtection",
			mock.Anything,
			appsec.UpdateAPIConstraintsProtectionRequest{ConfigID: 43253, Version: 7, PolicyID: "AAAA_81230"},
		).Return(&updateResponseAllProtectionsFalse, nil).Once()

		useClient(client, func() {
			resource.Test(t, resource.TestCase{
				PreCheck:   func() { testAccPreCheck(t) },
				IsUnitTest: true,
				Providers:  testAccProviders,
				Steps: []resource.TestStep{
					{
						Config: loadFixtureString("testdata/TestResAPIConstraintsProtection/match_by_id.tf"),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("akamai_appsec_api_constraints_protection.test", "id", "43253:AAAA_81230"),
							resource.TestCheckResourceAttr("akamai_appsec_api_constraints_protection.test", "enabled", "false"),
						),
					},
					{
						Config: loadFixtureString("testdata/TestResAPIConstraintsProtection/update_by_id.tf"),
						Check: resource.ComposeTestCheckFunc(
							resource.TestCheckResourceAttr("akamai_appsec_api_constraints_protection.test", "id", "43253:AAAA_81230"),
							resource.TestCheckResourceAttr("akamai_appsec_api_constraints_protection.test", "enabled", "true"),
						),
					},
				},
			})
		})

		client.AssertExpectations(t)
	})

}
