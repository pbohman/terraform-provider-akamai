package appsec

import (
	"context"
	"errors"
	"strconv"

	v2 "github.com/akamai/AkamaiOPEN-edgegrid-golang/v2/pkg/appsec"
	"github.com/akamai/terraform-provider-akamai/v2/pkg/akamai"
	"github.com/akamai/terraform-provider-akamai/v2/pkg/tools"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// appsec v1
//
// https://developer.akamai.com/api/cloud_security/application_security/v1.html
func resourceWAFProtection() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceWAFProtectionUpdate,
		ReadContext:   resourceWAFProtectionRead,
		UpdateContext: resourceWAFProtectionUpdate,
		DeleteContext: resourceWAFProtectionDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
		Schema: map[string]*schema.Schema{
			"config_id": {
				Type:     schema.TypeInt,
				Required: true,
			},
			"version": {
				Type:     schema.TypeInt,
				Required: true,
			},
			"policy_id": {
				Type:     schema.TypeString,
				Required: true,
			},
			"enabled": {
				Type:     schema.TypeBool,
				Required: true,
			},
			"output_text": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "Text Export representation",
			},
		},
	}
}

func resourceWAFProtectionRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	meta := akamai.Meta(m)
	client := inst.Client(meta)
	logger := meta.Log("APPSEC", "resourceWAFProtectionRead")

	getWAFProtection := v2.GetWAFProtectionRequest{}

	configid, err := tools.GetIntValue("config_id", d)
	if err != nil && !errors.Is(err, tools.ErrNotFound) {
		return diag.FromErr(err)
	}
	getWAFProtection.ConfigID = configid

	version, err := tools.GetIntValue("version", d)
	if err != nil && !errors.Is(err, tools.ErrNotFound) {
		return diag.FromErr(err)
	}
	getWAFProtection.Version = version

	policyid, err := tools.GetStringValue("policy_id", d)
	if err != nil && !errors.Is(err, tools.ErrNotFound) {
		return diag.FromErr(err)
	}
	getWAFProtection.PolicyID = policyid

	wafprotection, err := client.GetWAFProtection(ctx, getWAFProtection)
	if err != nil {
		logger.Errorf("calling 'getWAFProtection': %s", err.Error())
		return diag.FromErr(err)
	}

	ots := OutputTemplates{}
	InitTemplates(ots)

	outputtext, err := RenderTemplates(ots, "wafProtectionDS", wafprotection)
	if err == nil {
		d.Set("output_text", outputtext)
	}

	d.SetId(strconv.Itoa(getWAFProtection.ConfigID))

	return nil
}

func resourceWAFProtectionDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {

	return schema.NoopContext(nil, d, m)
}

func resourceWAFProtectionUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	meta := akamai.Meta(m)
	client := inst.Client(meta)
	logger := meta.Log("APPSEC", "resourceWAFProtectionUpdate")

	updateWAFProtection := v2.UpdateWAFProtectionRequest{}

	configid, err := tools.GetIntValue("config_id", d)
	if err != nil && !errors.Is(err, tools.ErrNotFound) {
		return diag.FromErr(err)
	}
	updateWAFProtection.ConfigID = configid

	version, err := tools.GetIntValue("version", d)
	if err != nil && !errors.Is(err, tools.ErrNotFound) {
		return diag.FromErr(err)
	}
	updateWAFProtection.Version = version

	policyid, err := tools.GetStringValue("policy_id", d)
	if err != nil && !errors.Is(err, tools.ErrNotFound) {
		return diag.FromErr(err)
	}
	updateWAFProtection.PolicyID = policyid

	applyapplicationlayercontrols, err := tools.GetBoolValue("enabled", d)
	if err != nil && !errors.Is(err, tools.ErrNotFound) {
		return diag.FromErr(err)
	}
	updateWAFProtection.ApplyApplicationLayerControls = applyapplicationlayercontrols

	_, erru := client.UpdateWAFProtection(ctx, updateWAFProtection)
	if erru != nil {
		logger.Errorf("calling 'updateWAFProtection': %s", erru.Error())
		return diag.FromErr(erru)
	}

	return resourceWAFProtectionRead(ctx, d, m)
}
