package services

import (
	"strings"
	"testing"
	"time"

	"finance-bot/bot/models"
	"finance-bot/bot/repositories"
)

type fakeNetWorthRepo struct {
	assets           []*models.Asset
	liabilities      []*models.Liability
	history          []*models.NetWorthSnapshot
	totalAssets      int64
	totalLiabilities int64
	createAssetErr   error
}

func (f *fakeNetWorthRepo) CreateAsset(asset *models.Asset) error {
	if f.createAssetErr != nil {
		return f.createAssetErr
	}
	asset.ID = "asset-1"
	f.assets = append(f.assets, asset)
	return nil
}
func (f *fakeNetWorthRepo) UpdateAssetAmount(id string, amount int64) error { return nil }
func (f *fakeNetWorthRepo) DeleteAsset(id string) error                     { return nil }
func (f *fakeNetWorthRepo) GetAssetsByUser(userID string) ([]*models.Asset, error) {
	return f.assets, nil
}
func (f *fakeNetWorthRepo) CreateLiability(liability *models.Liability) error {
	liability.ID = "liab-1"
	f.liabilities = append(f.liabilities, liability)
	return nil
}
func (f *fakeNetWorthRepo) UpdateLiabilityAmount(id string, amount int64) error { return nil }
func (f *fakeNetWorthRepo) DeleteLiability(id string) error                     { return nil }
func (f *fakeNetWorthRepo) GetLiabilitiesByUser(userID string) ([]*models.Liability, error) {
	return f.liabilities, nil
}
func (f *fakeNetWorthRepo) CalculateNetWorth(userID string) (int64, int64, error) {
	var assetsTotal, liabilitiesTotal int64
	for _, a := range f.assets {
		assetsTotal += a.Amount
	}
	for _, l := range f.liabilities {
		liabilitiesTotal += l.Amount
	}
	return assetsTotal, liabilitiesTotal, nil
}
func (f *fakeNetWorthRepo) CreateNetWorthSnapshot(snapshot *models.NetWorthSnapshot) error {
	snapshot.ID = "snap-1"
	f.history = append(f.history, snapshot)
	return nil
}
func (f *fakeNetWorthRepo) GetNetWorthHistory(userID string) ([]*models.NetWorthSnapshot, error) {
	return f.history, nil
}

func TestFinanceService_NetWorthTracker(t *testing.T) {
	fakeUser := &models.User{ID: "user-1", TelegramID: 111}
	userRepo := &fakeUserRepo{user: fakeUser}
	netWorthRepo := &fakeNetWorthRepo{}

	svc := &financeService{
		userRepo:     userRepo,
		netWorthRepo: netWorthRepo,
		loc:          time.UTC,
	}

	// 1. Add Asset
	asset, err := svc.AddAsset(111, "investasi", "Saham BBCA", 10000000, "investasi jangka panjang")
	if err != nil {
		t.Fatalf("unexpected error adding asset: %v", err)
	}
	if asset.Name != "Saham BBCA" || asset.Amount != 10000000 {
		t.Errorf("asset properties mismatch, got Name: %s, Amount: %d", asset.Name, asset.Amount)
	}

	// 2. Add Liability
	liability, err := svc.AddLiability(111, "kartu_kredit", "CC BCA", 2000000, nil, "tagihan bulan juni")
	if err != nil {
		t.Fatalf("unexpected error adding liability: %v", err)
	}
	if liability.Name != "CC BCA" || liability.Amount != 2000000 {
		t.Errorf("liability properties mismatch, got Name: %s, Amount: %d", liability.Name, liability.Amount)
	}

	// 3. Get Net Worth Status
	status, err := svc.GetNetWorthStatus(111)
	if err != nil {
		t.Fatalf("unexpected error getting net worth status: %v", err)
	}

	if !strings.Contains(status, "Total Assets:\nRp 10.000.000") {
		t.Errorf("expected total assets Rp 10.000.000, got: %s", status)
	}
	if !strings.Contains(status, "Total Liabilities:\nRp 2.000.000") {
		t.Errorf("expected total liabilities Rp 2.000.000, got: %s", status)
	}
	if !strings.Contains(status, "Net Worth:\nRp 8.000.000") {
		t.Errorf("expected net worth Rp 8.000.000, got: %s", status)
	}

	// 4. Get Net Worth History
	history, err := svc.GetNetWorthHistory(111)
	if err != nil {
		t.Fatalf("unexpected error getting net worth history: %v", err)
	}
	if !strings.Contains(history, "Total Aset:* Rp 10.000.000") {
		t.Errorf("expected history to contain total asset, got: %s", history)
	}
}

func TestFinanceService_NetWorthWarnings(t *testing.T) {
	fakeUser := &models.User{ID: "user-1", TelegramID: 111}
	userRepo := &fakeUserRepo{user: fakeUser}
	
	// Case 1: Negative net worth
	netWorthRepo1 := &fakeNetWorthRepo{
		assets: []*models.Asset{
			{AssetType: "cash", Name: "Cash", Amount: 1000000},
		},
		liabilities: []*models.Liability{
			{LiabilityType: "loan", Name: "Debt", Amount: 1500000},
		},
	}
	svc1 := &financeService{userRepo: userRepo, netWorthRepo: netWorthRepo1, loc: time.UTC}
	status1, _ := svc1.GetNetWorthStatus(111)
	if !strings.Contains(status1, "Peringatan: Net worth kamu negatif!") {
		t.Errorf("expected warning for negative net worth, got: %s", status1)
	}

	// Case 2: Liabilities > 50% of assets
	netWorthRepo2 := &fakeNetWorthRepo{
		assets: []*models.Asset{
			{AssetType: "cash", Name: "Cash", Amount: 1000000},
		},
		liabilities: []*models.Liability{
			{LiabilityType: "loan", Name: "Debt", Amount: 600000},
		},
	}
	svc2 := &financeService{userRepo: userRepo, netWorthRepo: netWorthRepo2, loc: time.UTC}
	status2, _ := svc2.GetNetWorthStatus(111)
	if !strings.Contains(status2, "Peringatan Risiko: Total kewajiban kamu melebihi 50% dari total aset!") {
		t.Errorf("expected risk warning for liabilities > 50%% of assets, got: %s", status2)
	}
}

// Ensure interface compliance
var _ repositories.NetWorthRepository = (*fakeNetWorthRepo)(nil)
