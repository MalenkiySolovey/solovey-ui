package apply

import "gorm.io/gorm"

type ObjectApplier interface {
	RemoveOutbounds(tags []string) error
	RemoveEndpoints(tags []string) error
	RemoveInbounds(tags []string) error
	RemoveServices(tags []string) error
	RestartOutbounds(tx *gorm.DB, ids []uint) error
	RestartEndpoints(tx *gorm.DB, ids []uint) error
	RestartInbounds(tx *gorm.DB, ids []uint) error
	RestartServices(tx *gorm.DB, ids []uint) error
}

func ExecuteObjectChanges(tx *gorm.DB, plan Plan, applier ObjectApplier) error {
	if err := executeObjectRemovals(plan, applier); err != nil {
		return err
	}
	if outboundIDs := plan.OutboundIDs(); len(outboundIDs) > 0 {
		if err := applier.RestartOutbounds(tx, outboundIDs); err != nil {
			return err
		}
	}
	if endpointIDs := plan.EndpointIDs(); len(endpointIDs) > 0 {
		if err := applier.RestartEndpoints(tx, endpointIDs); err != nil {
			return err
		}
	}
	if inboundIDs := plan.InboundIDs(); len(inboundIDs) > 0 {
		if err := applier.RestartInbounds(tx, inboundIDs); err != nil {
			return err
		}
	}
	if serviceIDs := plan.ServiceIDs(); len(serviceIDs) > 0 {
		if err := applier.RestartServices(tx, serviceIDs); err != nil {
			return err
		}
	}
	return nil
}

func executeObjectRemovals(plan Plan, applier ObjectApplier) error {
	if removedOutboundTags := plan.RemovedOutboundTags(); len(removedOutboundTags) > 0 {
		if err := applier.RemoveOutbounds(removedOutboundTags); err != nil {
			return err
		}
	}
	if removedEndpointTags := plan.RemovedEndpointTags(); len(removedEndpointTags) > 0 {
		if err := applier.RemoveEndpoints(removedEndpointTags); err != nil {
			return err
		}
	}
	if removedInboundTags := plan.RemovedInboundTags(); len(removedInboundTags) > 0 {
		if err := applier.RemoveInbounds(removedInboundTags); err != nil {
			return err
		}
	}
	if removedServiceTags := plan.RemovedServiceTags(); len(removedServiceTags) > 0 {
		if err := applier.RemoveServices(removedServiceTags); err != nil {
			return err
		}
	}
	return nil
}
