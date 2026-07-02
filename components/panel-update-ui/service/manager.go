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
	if _, err := m.Runtime.Enable(ctx, id); err != nil {
		return ComponentStatus{}, err
	}
	return m.Catalog.StatusByID(id)
}

func (m Manager) Disable(ctx OperationContext, id string) (ComponentStatus, error) {
	if _, err := m.Runtime.Disable(ctx, id); err != nil {
		return ComponentStatus{}, err
	}
	return m.Catalog.StatusByID(id)
}

func (m Manager) Install(ctx OperationContext, id string) (ComponentStatus, error) {
	if _, err := m.Runtime.Install(ctx, id); err != nil {
		return ComponentStatus{}, err
	}
	return m.Catalog.StatusByID(id)
}

func (m Manager) Remove(ctx OperationContext, id string) (ComponentStatus, error) {
	if _, err := m.Runtime.Remove(ctx, id); err != nil {
		return ComponentStatus{}, err
	}
	return m.Catalog.StatusByID(id)
}
