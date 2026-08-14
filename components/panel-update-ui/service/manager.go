package service

type Manager struct {
	Catalog Catalog
	Runtime RuntimeManager
}

func NewManager(runtime RuntimeManager) Manager {
	return Manager{
		Catalog: NewCatalog(),
		Runtime: runtime,
	}
}

func (m Manager) Inventory() (Inventory, error) {
	return m.Catalog.Inventory()
}

func (m Manager) Enable(ctx OperationContext, id string) (ComponentStatus, error) {
	return m.Runtime.Enable(ctx, id)
}

func (m Manager) Disable(ctx OperationContext, id string) (ComponentStatus, error) {
	return m.Runtime.Disable(ctx, id)
}

func (m Manager) Install(ctx OperationContext, id string) (ComponentStatus, error) {
	return m.Runtime.Install(ctx, id)
}

func (m Manager) Remove(ctx OperationContext, id string) (ComponentStatus, error) {
	return m.Runtime.Remove(ctx, id)
}
