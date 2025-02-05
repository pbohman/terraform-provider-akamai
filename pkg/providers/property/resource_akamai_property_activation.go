package property

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akamai/AkamaiOPEN-edgegrid-golang/v3/pkg/papi"
	"github.com/akamai/AkamaiOPEN-edgegrid-golang/v3/pkg/session"
	"github.com/akamai/terraform-provider-akamai/v3/pkg/akamai"
	"github.com/akamai/terraform-provider-akamai/v3/pkg/tools"
	"github.com/apex/log"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/spf13/cast"
)

func resourcePropertyActivation() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourcePropertyActivationCreate,
		ReadContext:   resourcePropertyActivationRead,
		UpdateContext: resourcePropertyActivationUpdate,
		DeleteContext: resourcePropertyActivationDelete,
		Schema:        akamaiPropertyActivationSchema,
		Timeouts: &schema.ResourceTimeout{
			Default: &PropertyResourceTimeout,
		},
	}
}

const (
	// ActivationPollMinimum is the minimum polling interval for activation creation
	ActivationPollMinimum = time.Minute
)

var (
	// ActivationPollInterval is the interval for polling an activation status on creation
	ActivationPollInterval = ActivationPollMinimum

	// PropertyResourceTimeout is the default timeout for the resource operations
	PropertyResourceTimeout = time.Minute * 90
)

var akamaiPropertyActivationSchema = map[string]*schema.Schema{
	"property": {
		Type:       schema.TypeString,
		Optional:   true,
		Deprecated: akamai.NoticeDeprecatedUseAlias("property"),
		Computed:   true,
		StateFunc:  addPrefixToState("prp_"),
	},
	"property_id": {
		Type:         schema.TypeString,
		Optional:     true,
		ExactlyOneOf: []string{"property_id", "property"},
		Computed:     true,
		StateFunc:    addPrefixToState("prp_"),
	},
	"activation_id": {
		Type:     schema.TypeString,
		Optional: true,
		Computed: true,
	},
	"errors": {
		Type:     schema.TypeString,
		Computed: true,
	},
	"warnings": {
		Type:     schema.TypeString,
		Computed: true,
	},
	"rule_errors": {
		Type:     schema.TypeList,
		Optional: true,
		Computed: true,
		Elem:     papiError(),
	},
	"rule_warnings": {
		Type:       schema.TypeList,
		Optional:   true,
		Computed:   true,
		Elem:       papiError(),
		Deprecated: "Rule warnings will not be set in state anymore",
	},
	"auto_acknowledge_rule_warnings": {
		Type:        schema.TypeBool,
		Optional:    true,
		Default:     true,
		Description: "automatically acknowledge all rule warnings for activation to continue. default is true",
	},
	"version": {
		Type:             schema.TypeInt,
		Required:         true,
		ValidateDiagFunc: tools.IsNotBlank,
	},
	"network": {
		Type:     schema.TypeString,
		Optional: true,
		Default:  papi.ActivationNetworkStaging,
	},
	"contact": {
		Type:     schema.TypeSet,
		Required: true,
		Elem:     &schema.Schema{Type: schema.TypeString},
	},
	"status": {
		Type:     schema.TypeString,
		Computed: true,
	},
	"note": {
		Type:        schema.TypeString,
		Optional:    true,
		Description: "assigns a log message to the activation request",
	},
}

func papiError() *schema.Resource {
	return &schema.Resource{Schema: map[string]*schema.Schema{
		"type":           {Type: schema.TypeString, Optional: true},
		"title":          {Type: schema.TypeString, Optional: true},
		"detail":         {Type: schema.TypeString, Optional: true},
		"instance":       {Type: schema.TypeString, Optional: true},
		"behavior_name":  {Type: schema.TypeString, Optional: true},
		"error_location": {Type: schema.TypeString, Optional: true},
		"status_code":    {Type: schema.TypeInt, Optional: true},
	}}
}

func resourcePropertyActivationCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	meta := akamai.Meta(m)
	logger := meta.Log("PAPI", "resourcePropertyActivationCreate")
	client := inst.Client(meta)

	logger.Debug("resourcePropertyActivationCreate call")

	// create a context with logging for api calls
	ctx = session.ContextWithOptions(
		ctx,
		session.WithContextLog(logger),
	)

	if dead, ok := ctx.Deadline(); ok {
		logger.Debugf("activation create with deadline in %s", time.Until(dead).String())
	}

	propertyID, err := resolvePropertyID(d)
	if err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("property_id", propertyID); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	network, err := networkAlias(d)
	if err != nil {
		return diag.FromErr(err)
	}

	version, err := resolveVersion(ctx, d, client, propertyID, network)
	if err != nil {
		return diag.FromErr(err)
	}
	// Schema guarantees these types
	acknowledgeRuleWarnings := d.Get("auto_acknowledge_rule_warnings").(bool)

	// check to see if this tree has any issues
	rules, err := client.GetRuleTree(ctx, papi.GetRuleTreeRequest{
		PropertyID:      propertyID,
		PropertyVersion: version,
		ValidateRules:   true,
	})
	if err != nil {
		d.Partial(true)
		return diag.FromErr(err)
	}

	// if there are errors return them cleanly
	diags := checkRuleTreeErrorsAndWarnings(rules, d, logger)
	if diags != nil && diags.HasError() {
		d.Partial(true)
		return diags
	}
	activation, err := lookupActivation(ctx, client, lookupActivationRequest{
		propertyID: propertyID,
		version:    version,
		network:    network,
		activationType: map[papi.ActivationType]struct{}{
			papi.ActivationTypeActivate:   {},
			papi.ActivationTypeDeactivate: {},
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}

	// we create a new property activation in case of no previous activation, or deleted activation
	if activation == nil || activation.ActivationType == papi.ActivationTypeDeactivate {
		notifySet, err := tools.GetSetValue("contact", d)
		if err != nil {
			return diag.FromErr(err)
		}
		var notify []string
		for _, contact := range notifySet.List() {
			notify = append(notify, cast.ToString(contact))
		}

		note, err := tools.GetStringValue("note", d)
		if err != nil && !errors.Is(err, tools.ErrNotFound) {
			return diag.FromErr(err)
		}

		create, err := client.CreateActivation(ctx, papi.CreateActivationRequest{
			PropertyID: propertyID,
			Activation: papi.Activation{
				ActivationType:         papi.ActivationTypeActivate,
				Network:                network,
				PropertyVersion:        version,
				NotifyEmails:           notify,
				AcknowledgeAllWarnings: acknowledgeRuleWarnings,
				Note:                   note,
			},
		})
		if err != nil {
			return diag.FromErr(fmt.Errorf("create activation failed: %w", err))
		}

		// query the activation to retrieve the initial status
		act, err := client.GetActivation(ctx, papi.GetActivationRequest{
			ActivationID: create.ActivationID,
			PropertyID:   propertyID,
		})
		if err != nil {
			return diag.FromErr(err)
		}

		activation = act.Activation

		if err = setErrorsAndWarnings(d, flattenErrorArray(act.Errors), flattenErrorArray(act.Warnings)); err != nil {
			return diag.FromErr(err)
		}
	}

	if err := d.Set("activation_id", activation.ActivationID); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	for activation.Status != papi.ActivationStatusActive {
		if activation.Status == papi.ActivationStatusAborted {
			return diag.FromErr(fmt.Errorf("activation request aborted"))
		}
		if activation.Status == papi.ActivationStatusFailed {
			return diag.FromErr(fmt.Errorf("activation request failed in downstream system"))
		}
		select {
		case <-time.After(tools.MaxDuration(ActivationPollInterval, ActivationPollMinimum)):
			act, err := client.GetActivation(ctx, papi.GetActivationRequest{
				ActivationID: activation.ActivationID,
				PropertyID:   propertyID,
			})
			if err != nil {
				return diag.FromErr(err)
			}
			activation = act.Activation

		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return diag.Diagnostics{DiagWarnActivationTimeout}
			} else if errors.Is(ctx.Err(), context.Canceled) {
				return diag.Diagnostics{DiagWarnActivationCanceled}
			}
			return diag.FromErr(fmt.Errorf("activation context terminated: %w", ctx.Err()))
		}
	}

	if err := d.Set("status", string(activation.Status)); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	d.SetId(propertyID + ":" + string(network))

	if err := d.Set("version", version); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	return nil
}

func resourcePropertyActivationDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	meta := akamai.Meta(m)
	logger := meta.Log("PAPI", "resourcePropertyActivationDelete")
	client := inst.Client(meta)

	logger.Debug("resourcePropertyActivationDelete call")

	// create a context with logging for api calls
	ctx = session.ContextWithOptions(
		ctx,
		session.WithContextLog(logger),
	)

	network, err := networkAlias(d)
	if err != nil {
		return diag.FromErr(err)
	}

	propertyID, err := resolvePropertyID(d)
	if err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("property_id", propertyID); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	version, err := resolveVersion(ctx, d, client, propertyID, network)
	if err != nil {
		return diag.FromErr(err)
	}

	// Schema guarantees these types
	acknowledgeRuleWarnings := d.Get("auto_acknowledge_rule_warnings").(bool)

	activation, err := lookupActivation(ctx, client, lookupActivationRequest{
		propertyID: propertyID,
		version:    version,
		network:    network,
		activationType: map[papi.ActivationType]struct{}{
			papi.ActivationTypeDeactivate: {},
			papi.ActivationTypeActivate:   {},
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}

	if activation == nil || activation.ActivationType == papi.ActivationTypeActivate {
		notifySet, err := tools.GetSetValue("contact", d)
		if err != nil {
			return diag.FromErr(err)
		}
		var notify []string
		for _, contact := range notifySet.List() {
			notify = append(notify, cast.ToString(contact))
		}
		note, err := tools.GetStringValue("note", d)
		if err != nil && !errors.Is(err, tools.ErrNotFound) {
			return diag.FromErr(err)
		}

		deleteActivation, err := client.CreateActivation(ctx, papi.CreateActivationRequest{
			PropertyID: propertyID,
			Activation: papi.Activation{
				ActivationType:         papi.ActivationTypeDeactivate,
				Network:                network,
				PropertyVersion:        version,
				NotifyEmails:           notify,
				AcknowledgeAllWarnings: acknowledgeRuleWarnings,
				Note:                   note,
			},
		})
		if err != nil {
			return diag.FromErr(fmt.Errorf("create deactivation failed: %w", err))
		}
		// update with id we are now polling on
		d.SetId(deleteActivation.ActivationID)

		// query the activation to retrieve the initial status
		act, err := client.GetActivation(ctx, papi.GetActivationRequest{
			ActivationID: deleteActivation.ActivationID,
			PropertyID:   propertyID,
		})
		if err != nil {
			return diag.FromErr(err)
		}

		activation = act.Activation

		if err := d.Set("activation_id", activation.ActivationID); err != nil {
			return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
		}

		if err = setErrorsAndWarnings(d, flattenErrorArray(act.Errors), flattenErrorArray(act.Warnings)); err != nil {
			return diag.FromErr(err)
		}
	}

	// deactivations also use status Active for when they are fully processed
	for activation.Status != papi.ActivationStatusActive {
		if activation.Status == papi.ActivationStatusAborted {
			return diag.FromErr(fmt.Errorf("deactivation request aborted"))
		}
		if activation.Status == papi.ActivationStatusFailed {
			return diag.FromErr(fmt.Errorf("deactivation request failed in downstream system"))
		}
		select {
		case <-time.After(tools.MaxDuration(ActivationPollInterval, ActivationPollMinimum)):
			act, err := client.GetActivation(ctx, papi.GetActivationRequest{
				ActivationID: activation.ActivationID,
				PropertyID:   propertyID,
			})
			if err != nil {
				return diag.FromErr(err)
			}
			activation = act.Activation

			if err = setErrorsAndWarnings(d, flattenErrorArray(act.Errors), flattenErrorArray(act.Warnings)); err != nil {
				return diag.FromErr(err)
			}

		case <-ctx.Done():
			return diag.FromErr(fmt.Errorf("activation context terminated: %w", ctx.Err()))
		}
	}

	d.SetId("")

	return nil
}

func flattenErrorArray(errors []*papi.Error) string {
	var errorStrArr = make([]string, len(errors))
	for i, err := range errors {
		strError := tools.ConvertToString(err.Error())
		errorStrArr[i] = strError
	}
	return strings.Join(errorStrArr, "\n")
}

func resourcePropertyActivationRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	meta := akamai.Meta(m)
	logger := meta.Log("PAPI", "resourcePropertyActivationRead")
	client := inst.Client(meta)

	logger.Debug("resourcePropertyActivationRead call")
	// create a context with logging for api calls
	ctx = session.ContextWithOptions(
		ctx,
		session.WithContextLog(logger),
	)

	propertyID, err := resolvePropertyID(d)
	if err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("property_id", propertyID); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	network, err := networkAlias(d)
	if err != nil {
		return diag.FromErr(err)
	}

	version, err := resolveVersion(ctx, d, client, propertyID, network)
	if err != nil {
		return diag.FromErr(err)
	}

	resp, err := client.GetActivations(ctx, papi.GetActivationsRequest{
		PropertyID: propertyID,
	})
	if err != nil {
		return diag.FromErr(fmt.Errorf("failed to get activations for property: %w", err))
	}

	if err := d.Set("errors", flattenErrorArray(resp.Errors)); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}
	if err := d.Set("warnings", flattenErrorArray(resp.Warnings)); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	for _, act := range resp.Activations.Items {

		if act.Network == network && act.PropertyVersion == version {
			logger.Debugf("Found Existing Activation %s version %d", network, version)

			if err := d.Set("status", string(act.Status)); err != nil {
				return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
			}
			if err := d.Set("version", act.PropertyVersion); err != nil {
				return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
			}

			d.SetId(act.PropertyID + ":" + string(network))

			if err := d.Set("version", version); err != nil {
				return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
			}
			if err := d.Set("activation_id", act.ActivationID); err != nil {
				return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
			}

			break
		}
	}

	return nil
}

func resolveVersion(ctx context.Context, d *schema.ResourceData, client papi.PAPI, propertyID string, network papi.ActivationNetwork) (int, error) {

	version, err := tools.GetIntValue("version", d)
	if err != nil {
		var resp *papi.GetPropertyVersionsResponse
		// use the latest version for the property
		resp, err = client.GetLatestVersion(ctx, papi.GetLatestVersionRequest{
			PropertyID:  propertyID,
			ActivatedOn: fmt.Sprintf("%v", network),
		})
		if err != nil {
			return 0, err
		}
		version = resp.Version.PropertyVersion
	}

	return version, nil
}

func resourcePropertyActivationUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	meta := akamai.Meta(m)
	logger := meta.Log("PAPI", "resourcePropertyActivationUpdate")
	client := inst.Client(meta)

	logger.Debug("resourcePropertyActivationUpdate call")
	// create a context with logging for api calls
	ctx = session.ContextWithOptions(
		ctx,
		session.WithContextLog(logger),
	)

	propertyID, err := resolvePropertyID(d)
	if err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("property_id", propertyID); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	network, err := networkAlias(d)
	if err != nil {
		return diag.FromErr(err)
	}

	version, err := resolveVersion(ctx, d, client, propertyID, network)
	if err != nil {
		return diag.FromErr(err)
	}

	// Schema guarantees these types
	acknowledgeRuleWarnings := d.Get("auto_acknowledge_rule_warnings").(bool)

	// Assigns a log message to the activation request
	note, err := tools.GetStringValue("note", d)
	if err != nil && !errors.Is(err, tools.ErrNotFound) {
		return diag.FromErr(err)
	}

	// check to see if this tree has any issues
	rules, err := client.GetRuleTree(ctx, papi.GetRuleTreeRequest{
		PropertyID:      propertyID,
		PropertyVersion: version,
		ValidateRules:   true,
	})
	if err != nil {
		// Reverting to previous state(property version in this case) when error occurs.
		d.Partial(true)
		return diag.FromErr(err)
	}

	// if there are errors return them cleanly
	diags := checkRuleTreeErrorsAndWarnings(rules, d, logger)
	if diags.HasError() {
		d.Partial(true)
		return diags
	}
	propertyActivation, err := lookupActivation(ctx, client, lookupActivationRequest{
		propertyID: propertyID,
		version:    version,
		network:    network,
		activationType: map[papi.ActivationType]struct{}{
			papi.ActivationTypeActivate: {},
		},
	})
	if err != nil {
		return diag.FromErr(err)
	}

	versionStatus, err := resolveVersionStatus(ctx, client, propertyID, version, network)
	if err != nil {
		return diag.FromErr(err)
	}

	if propertyActivation == nil || versionStatus == papi.VersionStatusDeactivated {
		notifySet, err := tools.GetSetValue("contact", d)
		if err != nil {
			return diag.FromErr(err)
		}
		var notify []string
		for _, contact := range notifySet.List() {
			notify = append(notify, cast.ToString(contact))
		}

		create, err := client.CreateActivation(ctx, papi.CreateActivationRequest{
			PropertyID: propertyID,
			Activation: papi.Activation{
				ActivationType:         papi.ActivationTypeActivate,
				Network:                network,
				PropertyVersion:        version,
				NotifyEmails:           notify,
				AcknowledgeAllWarnings: acknowledgeRuleWarnings,
				Note:                   note,
			},
		})
		if err != nil {
			return diag.FromErr(fmt.Errorf("create activation failed: %w", err))
		}

		// query the activation to retrieve the initial status
		act, err := client.GetActivation(ctx, papi.GetActivationRequest{
			ActivationID: create.ActivationID,
			PropertyID:   propertyID,
		})
		if err != nil {
			return diag.FromErr(err)
		}

		propertyActivation = act.Activation

		if err = setErrorsAndWarnings(d, flattenErrorArray(act.Errors), flattenErrorArray(act.Warnings)); err != nil {
			return diag.FromErr(err)
		}
	} else if d.HasChange("note") {
		oldValue, _ := d.GetChange("note")
		if err = d.Set("note", oldValue); err != nil {
			return diag.FromErr(err)
		}
		return diag.Errorf("cannot update activation attribute note after creation")
	}

	for propertyActivation.Status != papi.ActivationStatusActive {
		if propertyActivation.Status == papi.ActivationStatusAborted {
			return diag.FromErr(fmt.Errorf("activation request aborted"))
		}
		if propertyActivation.Status == papi.ActivationStatusFailed {
			return diag.FromErr(fmt.Errorf("activation request failed in downstream system"))
		}
		select {
		case <-time.After(tools.MaxDuration(ActivationPollInterval, ActivationPollMinimum)):
			act, err := client.GetActivation(ctx, papi.GetActivationRequest{
				ActivationID: propertyActivation.ActivationID,
				PropertyID:   propertyID,
			})
			if err != nil {
				return diag.FromErr(err)
			}
			propertyActivation = act.Activation

			if err = setErrorsAndWarnings(d, flattenErrorArray(act.Errors), flattenErrorArray(act.Warnings)); err != nil {
				return diag.FromErr(err)
			}

		case <-ctx.Done():
			return diag.FromErr(fmt.Errorf("activation context terminated: %w", ctx.Err()))
		}
	}

	if err := d.Set("status", string(propertyActivation.Status)); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	d.SetId(propertyID + ":" + string(network))

	if err := d.Set("version", version); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	if err := d.Set("activation_id", propertyActivation.ActivationID); err != nil {
		return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
	}

	return nil
}

func resolveVersionStatus(ctx context.Context, client papi.PAPI, propertyID string, version int, network papi.ActivationNetwork) (papi.VersionStatus, error) {
	var versionStatus papi.VersionStatus
	propertyVersion, err := client.GetPropertyVersion(ctx, papi.GetPropertyVersionRequest{
		PropertyID:      propertyID,
		PropertyVersion: version,
	})
	if err != nil {
		return "", err
	}
	if network == papi.ActivationNetworkProduction {
		versionStatus = propertyVersion.Version.ProductionStatus
	} else {
		versionStatus = propertyVersion.Version.StagingStatus
	}
	return versionStatus, nil
}

func checkRuleTreeErrorsAndWarnings(rules *papi.GetRuleTreeResponse, d *schema.ResourceData, logger log.Interface) diag.Diagnostics {
	var diags diag.Diagnostics
	if len(rules.Errors) > 0 {
		if err := d.Set("rule_errors", papiErrorsToList(rules.Errors)); err != nil {
			return diag.FromErr(fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error()))
		}
		msg, err := json.MarshalIndent(papiErrorsToList(rules.Errors), "", "\t")
		if err != nil {
			return diag.FromErr(fmt.Errorf("error marshaling API error: %s", err))
		}
		logger.Errorf("Property has rule errors %s", msg)
		diags = append(diags, diag.Errorf("activation cannot continue due to rule errors: %s", msg)...)
	}
	if len(rules.Warnings) > 0 {
		msg, err := json.MarshalIndent(papiErrorsToList(rules.Warnings), "", "\t")
		if err != nil {
			return diag.FromErr(fmt.Errorf("error marshaling API warnings: %s", err))
		}
		logger.Warnf("Property has rule warnings %s", msg)
	}
	return diags
}

func setErrorsAndWarnings(d *schema.ResourceData, errors, warnings string) error {
	if err := d.Set("errors", errors); err != nil {
		return fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error())
	}
	if err := d.Set("warnings", warnings); err != nil {
		return fmt.Errorf("%w: %s", tools.ErrValueSet, err.Error())
	}
	return nil
}

func resolvePropertyID(d *schema.ResourceData) (string, error) {
	propertyID, err := tools.GetStringValue("property_id", d)
	if errors.Is(err, tools.ErrNotFound) {
		// use legacy property as fallback option
		propertyID, err = tools.GetStringValue("property", d)
	}
	return tools.AddPrefix(propertyID, "prp_"), err
}

type lookupActivationRequest struct {
	propertyID     string
	version        int
	network        papi.ActivationNetwork
	activationType map[papi.ActivationType]struct{}
}

// returns the most recent property activation (by SubmitDate) for the given query.
func lookupActivation(ctx context.Context, client papi.PAPI, query lookupActivationRequest) (*papi.Activation, error) {
	activations, err := client.GetActivations(ctx, papi.GetActivationsRequest{
		PropertyID: query.propertyID,
	})
	if err != nil {
		return nil, err
	}

	inProgressStates := map[papi.ActivationStatus]struct{}{
		papi.ActivationStatusActive:       {},
		papi.ActivationStatusNew:          {},
		papi.ActivationStatusPending:      {},
		papi.ActivationStatusDeactivating: {},
		papi.ActivationStatusZone1:        {},
		papi.ActivationStatusZone2:        {},
		papi.ActivationStatusZone3:        {},
	}

	var bestMatch *papi.Activation
	var bestMatchSubmitDate time.Time

	for _, a := range activations.Activations.Items {
		if _, ok := inProgressStates[a.Status]; !ok {
			continue
		}

		// There is an activation in progress, if it's for the same version/network/type we can re-use it
		_, matchingActivationType := query.activationType[a.ActivationType]
		if a.PropertyVersion == query.version && matchingActivationType && a.Network == query.network {
			// find the most recent activation

			var aSubmitDate, err = tools.ParseDate(tools.DateTimeFormat, a.SubmitDate)
			if err != nil {
				return nil, err
			}

			if bestMatchSubmitDate.IsZero() || bestMatchSubmitDate.Before(aSubmitDate) {
				bestMatch = a
				bestMatchSubmitDate = aSubmitDate
			}
		}
	}
	// only return activation if bestMatch.ActivationType == query.ActivationType

	if bestMatch != nil {
		_, matchingActivationType := query.activationType[bestMatch.ActivationType]
		if matchingActivationType {
			return bestMatch, nil
		}
	}
	return nil, nil
}

func networkAlias(d *schema.ResourceData) (papi.ActivationNetwork, error) {
	network, err := tools.GetStringValue("network", d)
	if err != nil {
		if errors.Is(err, tools.ErrNotFound) {
			network = "STAGING"
		} else {
			return "", err
		}
	}

	alias, err := NetworkAlias(network)
	if err != nil {
		return "", err
	}

	return papi.ActivationNetwork(alias), nil
}
