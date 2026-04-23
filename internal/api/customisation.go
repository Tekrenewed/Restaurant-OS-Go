package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// ─── Response Types ──────────────────────────────────────────────────────────

type VariantResponse struct {
	ID            string  `json:"id"             db:"id"`
	Name          string  `json:"name"           db:"name"`
	PriceOverride float64 `json:"price_override" db:"price_override"`
	IsDefault     bool    `json:"is_default"     db:"is_default"`
	SortOrder     int     `json:"sort_order"     db:"sort_order"`
	IsActive      bool    `json:"is_active"      db:"is_active"`
}

type ModifierOptionResponse struct {
	ID          string  `json:"id"           db:"id"`
	Name        string  `json:"name"         db:"name"`
	PriceDelta  float64 `json:"price_delta"  db:"price_delta"`
	IsDefault   bool    `json:"is_default"   db:"is_default"`
	IsAvailable bool    `json:"is_available" db:"is_available"`
	SortOrder   int     `json:"sort_order"   db:"sort_order"`
}

type ModifierGroupResponse struct {
	ID            string                   `json:"id"             db:"id"`
	Name          string                   `json:"name"           db:"name"`
	Description   string                   `json:"description"    db:"description"`
	IsRequired    bool                     `json:"is_required"    db:"is_required"`
	MinSelections int                      `json:"min_selections" db:"min_selections"`
	MaxSelections int                      `json:"max_selections" db:"max_selections"`
	SortOrder     int                      `json:"sort_order"     db:"sort_order"`
	Options       []ModifierOptionResponse `json:"options"`
}

type AllergenResponse struct {
	Allergen string `json:"allergen" db:"allergen"`
	Severity string `json:"severity" db:"severity"`
}

type NutritionResponse struct {
	ServingSizeG *int     `json:"serving_size_g,omitempty" db:"serving_size_g"`
	Calories     *int     `json:"calories,omitempty"       db:"calories"`
	FatG         *float64 `json:"fat_g,omitempty"          db:"fat_g"`
	SaturatesG   *float64 `json:"saturates_g,omitempty"    db:"saturates_g"`
	CarbsG       *float64 `json:"carbs_g,omitempty"        db:"carbs_g"`
	SugarsG      *float64 `json:"sugars_g,omitempty"       db:"sugars_g"`
	FibreG       *float64 `json:"fibre_g,omitempty"        db:"fibre_g"`
	ProteinG     *float64 `json:"protein_g,omitempty"      db:"protein_g"`
	SaltG        *float64 `json:"salt_g,omitempty"         db:"salt_g"`
}

type FullProductResponse struct {
	ID             string                  `json:"id"`
	BrandID        string                  `json:"brand_id"`
	Name           string                  `json:"name"`
	Description    string                  `json:"description"`
	Category       string                  `json:"category"`
	BasePrice      float64                 `json:"base_price"`
	ImageURL       string                  `json:"image_url"`
	Is86d          bool                    `json:"is_86d"`
	IsActive       bool                    `json:"is_active"`
	Variants       []VariantResponse       `json:"variants"`
	ModifierGroups []ModifierGroupResponse `json:"modifier_groups"`
	Allergens      []AllergenResponse      `json:"allergens"`
	Nutrition      *NutritionResponse      `json:"nutrition,omitempty"`
}

// ─── GET /api/v1/products/:id/full ───────────────────────────────────────────
// Returns a single product with all customisation data (variants, modifiers, allergens, nutrition)
func (s *Server) HandleGetFullProduct(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, `{"error":"bad_request","message":"Missing product ID"}`, http.StatusBadRequest)
		return
	}
	productID := parts[4]

	product, err := s.fetchFullProduct(productID)
	if err != nil {
		log.Printf("[customisation] GetFullProduct error: %v", err)
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(product)
}

// ─── GET /api/v1/catalog (ENHANCED) ─────────────────────────────────────────
// Returns full catalog with variants and allergens for every product
// This replaces the basic HandleGetCatalog (products only)
func (s *Server) HandleGetFullCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60") // 60s CDN cache

	if s.DB == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "db_unavailable"})
		return
	}

	// 1. Fetch all active products
	type rawProduct struct {
		ID          string  `db:"id"`
		BrandID     string  `db:"brand_id"`
		Name        string  `db:"name"`
		Description string  `db:"description"`
		Category    string  `db:"category"`
		BasePrice   float64 `db:"base_price"`
		ImageURL    string  `db:"image_url"`
		Is86d       bool    `db:"is_86d"`
		IsActive    bool    `db:"is_active"`
	}

	var rawProducts []rawProduct
	err := s.DB.Select(&rawProducts, `
		SELECT p.id, p.brand_id, p.name, 
		       COALESCE(p.description, '') as description, 
		       p.category,
		       COALESCE(p.image_url, '') as image_url,
		       p.is_86d, COALESCE(p.is_active, true) as is_active,
		       COALESCE(MAX(pp.price_amount), 0) as base_price
		FROM products p
		LEFT JOIN product_prices pp ON p.id = pp.product_id AND pp.price_level_id = 1
		WHERE COALESCE(p.is_active, true) = true
		GROUP BY p.id, p.brand_id, p.name, p.description, p.category, p.image_url, p.is_86d, p.is_active
		ORDER BY p.category, p.name
	`)
	if err != nil {
		log.Printf("[catalog] Failed to fetch products: %v", err)
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}

	if len(rawProducts) == 0 {
		json.NewEncoder(w).Encode([]FullProductResponse{})
		return
	}

	// 3. Fetch ALL variants (join to filter only active products)
	var variants []struct {
		ProductID string `db:"product_id"`
		VariantResponse
	}
	s.DB.Select(&variants, `
		SELECT pv.product_id, pv.id, pv.name, pv.price_override, pv.is_default, pv.sort_order, pv.is_active
		FROM product_variants pv
		INNER JOIN products p ON pv.product_id = p.id
		WHERE COALESCE(p.is_active, true) = true AND pv.is_active = true
		ORDER BY pv.product_id, pv.sort_order
	`)
	variantsByProduct := map[string][]VariantResponse{}
	for _, v := range variants {
		variantsByProduct[v.ProductID] = append(variantsByProduct[v.ProductID], v.VariantResponse)
	}

	// 4. Fetch ALL allergens
	var allergens []struct {
		ProductID string `db:"product_id"`
		AllergenResponse
	}
	s.DB.Select(&allergens, `
		SELECT pa.product_id, pa.allergen, pa.severity
		FROM product_allergens pa
		INNER JOIN products p ON pa.product_id = p.id
		WHERE COALESCE(p.is_active, true) = true
		ORDER BY pa.product_id, pa.allergen
	`)
	allergensByProduct := map[string][]AllergenResponse{}
	for _, a := range allergens {
		allergensByProduct[a.ProductID] = append(allergensByProduct[a.ProductID], a.AllergenResponse)
	}

	// 5. Fetch ALL modifier groups
	var groups []struct {
		ProductID string `db:"product_id"`
		ModifierGroupResponse
	}
	s.DB.Select(&groups, `
		SELECT mg.product_id, mg.id, mg.name, COALESCE(mg.description,'') as description,
		       mg.is_required, mg.min_selections, mg.max_selections, mg.sort_order
		FROM modifier_groups mg
		INNER JOIN products p ON mg.product_id = p.id
		WHERE COALESCE(p.is_active, true) = true AND mg.is_active = true
		ORDER BY mg.product_id, mg.sort_order
	`)

	// Build lookup: productID → []ModifierGroupResponse, groupID → &ModifierGroupResponse
	groupsByProduct := map[string][]ModifierGroupResponse{}
	groupIdxMap := map[string]struct{ pid string; idx int }{}
	for _, g := range groups {
		idx := len(groupsByProduct[g.ProductID])
		g.ModifierGroupResponse.Options = []ModifierOptionResponse{}
		groupsByProduct[g.ProductID] = append(groupsByProduct[g.ProductID], g.ModifierGroupResponse)
		groupIdxMap[g.ID] = struct{ pid string; idx int }{g.ProductID, idx}
	}

	// 5b. Fetch ALL modifier options
	var options []struct {
		GroupID string `db:"group_id"`
		ModifierOptionResponse
	}
	s.DB.Select(&options, `
		SELECT mo.group_id, mo.id, mo.name, mo.price_delta, mo.is_default, mo.is_available, mo.sort_order
		FROM modifier_options mo
		INNER JOIN modifier_groups mg ON mo.group_id = mg.id
		WHERE mo.is_available = true AND mg.is_active = true
		ORDER BY mo.group_id, mo.sort_order
	`)
	for _, opt := range options {
		if ref, ok := groupIdxMap[opt.GroupID]; ok {
			groupsByProduct[ref.pid][ref.idx].Options = append(
				groupsByProduct[ref.pid][ref.idx].Options, opt.ModifierOptionResponse,
			)
		}
	}

	// 6. Assemble final response
	result := make([]FullProductResponse, len(rawProducts))
	for i, p := range rawProducts {
		v := variantsByProduct[p.ID]
		if v == nil {
			v = []VariantResponse{}
		}
		mg := groupsByProduct[p.ID]
		if mg == nil {
			mg = []ModifierGroupResponse{}
		}
		al := allergensByProduct[p.ID]
		if al == nil {
			al = []AllergenResponse{}
		}
		result[i] = FullProductResponse{
			ID:             p.ID,
			BrandID:        p.BrandID,
			Name:           p.Name,
			Description:    p.Description,
			Category:       p.Category,
			BasePrice:      p.BasePrice,
			ImageURL:       p.ImageURL,
			Is86d:          p.Is86d,
			IsActive:       p.IsActive,
			Variants:       v,
			ModifierGroups: mg,
			Allergens:      al,
		}
	}

	json.NewEncoder(w).Encode(result)
}


// ─── POST /api/v1/products/:id/variants ──────────────────────────────────────
func (s *Server) HandleAddVariant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Missing product ID", http.StatusBadRequest)
		return
	}
	productID := parts[4]

	var req struct {
		Name          string  `json:"name"`
		PriceOverride float64 `json:"price_override"`
		IsDefault     bool    `json:"is_default"`
		SortOrder     int     `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if req.IsDefault {
		// Unset other defaults for this product first
		s.DB.Exec(`UPDATE product_variants SET is_default = false WHERE product_id = $1`, productID)
	}

	var id string
	err := s.DB.QueryRow(`
		INSERT INTO product_variants (product_id, name, price_override, is_default, sort_order, is_active)
		VALUES ($1,$2,$3,$4,$5,true)
		ON CONFLICT (product_id, name) DO UPDATE SET price_override=EXCLUDED.price_override, is_default=EXCLUDED.is_default
		RETURNING id`, productID, req.Name, req.PriceOverride, req.IsDefault, req.SortOrder,
	).Scan(&id)

	if err != nil {
		log.Printf("[customisation] AddVariant error: %v", err)
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "created"})
}

// ─── POST /api/v1/products/:id/groups ────────────────────────────────────────
func (s *Server) HandleAddModifierGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Missing product ID", http.StatusBadRequest)
		return
	}
	productID := parts[4]

	var req struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		IsRequired    bool   `json:"is_required"`
		MinSelections int    `json:"min_selections"`
		MaxSelections int    `json:"max_selections"`
		SortOrder     int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if req.MaxSelections == 0 {
		req.MaxSelections = 1
	}

	var id string
	err := s.DB.QueryRow(`
		INSERT INTO modifier_groups (product_id,name,description,is_required,min_selections,max_selections,sort_order,is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,true) RETURNING id`,
		productID, req.Name, req.Description, req.IsRequired, req.MinSelections, req.MaxSelections, req.SortOrder,
	).Scan(&id)

	if err != nil {
		log.Printf("[customisation] AddGroup error: %v", err)
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "created"})
}

// ─── POST /api/v1/groups/:id/options ─────────────────────────────────────────
func (s *Server) HandleAddModifierOption(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Missing group ID", http.StatusBadRequest)
		return
	}
	groupID := parts[4]

	var req struct {
		Name       string  `json:"name"`
		PriceDelta float64 `json:"price_delta"`
		IsDefault  bool    `json:"is_default"`
		SortOrder  int     `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	var id string
	err := s.DB.QueryRow(`
		INSERT INTO modifier_options (group_id,name,price_delta,is_default,is_available,sort_order)
		VALUES ($1,$2,$3,$4,true,$5) RETURNING id`,
		groupID, req.Name, req.PriceDelta, req.IsDefault, req.SortOrder,
	).Scan(&id)

	if err != nil {
		log.Printf("[customisation] AddOption error: %v", err)
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "created"})
}

// ─── PATCH /api/v1/options/:id ────────────────────────────────────────────────
func (s *Server) HandleUpdateModifierOption(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Missing option ID", http.StatusBadRequest)
		return
	}
	optionID := parts[4]

	var req struct {
		Name        *string  `json:"name"`
		PriceDelta  *float64 `json:"price_delta"`
		IsAvailable *bool    `json:"is_available"`
		IsDefault   *bool    `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		s.DB.Exec(`UPDATE modifier_options SET name=$1 WHERE id=$2`, *req.Name, optionID)
	}
	if req.PriceDelta != nil {
		s.DB.Exec(`UPDATE modifier_options SET price_delta=$1 WHERE id=$2`, *req.PriceDelta, optionID)
	}
	if req.IsAvailable != nil {
		s.DB.Exec(`UPDATE modifier_options SET is_available=$1 WHERE id=$2`, *req.IsAvailable, optionID)
	}
	if req.IsDefault != nil {
		s.DB.Exec(`UPDATE modifier_options SET is_default=$1 WHERE id=$2`, *req.IsDefault, optionID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// ─── POST /api/v1/products/:id/allergens ─────────────────────────────────────
func (s *Server) HandleSetAllergens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Missing product ID", http.StatusBadRequest)
		return
	}
	productID := parts[4]

	var req []struct {
		Allergen string `json:"allergen"`
		Severity string `json:"severity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	// Replace all allergens for this product
	tx, _ := s.DB.Beginx()
	tx.Exec(`DELETE FROM product_allergens WHERE product_id = $1`, productID)
	for _, a := range req {
		severity := a.Severity
		if severity == "" {
			severity = "contains"
		}
		tx.Exec(`INSERT INTO product_allergens (product_id,allergen,severity) VALUES ($1,$2,$3)
		          ON CONFLICT DO NOTHING`, productID, a.Allergen, severity)
	}
	tx.Commit()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated", "count": string(rune(len(req)))})
}

// ─── POST /api/v1/products/:id/nutrition ─────────────────────────────────────
func (s *Server) HandleSetNutrition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Missing product ID", http.StatusBadRequest)
		return
	}
	productID := parts[4]

	var req NutritionResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	_, err := s.DB.Exec(`
		INSERT INTO product_nutrition (product_id,serving_size_g,calories,fat_g,saturates_g,carbs_g,sugars_g,fibre_g,protein_g,salt_g)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (product_id) DO UPDATE SET
		  serving_size_g=EXCLUDED.serving_size_g, calories=EXCLUDED.calories,
		  fat_g=EXCLUDED.fat_g, saturates_g=EXCLUDED.saturates_g,
		  carbs_g=EXCLUDED.carbs_g, sugars_g=EXCLUDED.sugars_g,
		  fibre_g=EXCLUDED.fibre_g, protein_g=EXCLUDED.protein_g, salt_g=EXCLUDED.salt_g`,
		productID, req.ServingSizeG, req.Calories, req.FatG, req.SaturatesG,
		req.CarbsG, req.SugarsG, req.FibreG, req.ProteinG, req.SaltG,
	)
	if err != nil {
		log.Printf("[customisation] SetNutrition error: %v", err)
		http.Error(w, `{"error":"db_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// ─── Helper: fetchFullProduct ────────────────────────────────────────────────
func (s *Server) fetchFullProduct(productID string) (*FullProductResponse, error) {
	type rawProduct struct {
		ID          string  `db:"id"`
		BrandID     string  `db:"brand_id"`
		Name        string  `db:"name"`
		Description string  `db:"description"`
		Category    string  `db:"category"`
		BasePrice   float64 `db:"base_price"`
		ImageURL    string  `db:"image_url"`
		Is86d       bool    `db:"is_86d"`
		IsActive    bool    `db:"is_active"`
	}
	var p rawProduct
	err := s.DB.Get(&p, `
		SELECT p.id, p.brand_id, p.name,
		       COALESCE(p.description,'') as description, p.category,
		       COALESCE(p.image_url,'') as image_url,
		       p.is_86d, COALESCE(p.is_active,true) as is_active,
		       COALESCE(MAX(pp.price_amount),0) as base_price
		FROM products p
		LEFT JOIN product_prices pp ON p.id = pp.product_id AND pp.price_level_id = 1
		WHERE p.id = $1 GROUP BY p.id`, productID)
	if err != nil {
		return nil, err
	}

	var variants []VariantResponse
	s.DB.Select(&variants, `SELECT id,name,price_override,is_default,sort_order,is_active
		FROM product_variants WHERE product_id=$1 AND is_active=true ORDER BY sort_order`, productID)

	var allergens []AllergenResponse
	s.DB.Select(&allergens, `SELECT allergen,severity FROM product_allergens WHERE product_id=$1 ORDER BY allergen`, productID)

	var groups []ModifierGroupResponse
	s.DB.Select(&groups, `SELECT id,name,COALESCE(description,'') as description,is_required,min_selections,max_selections,sort_order
		FROM modifier_groups WHERE product_id=$1 AND is_active=true ORDER BY sort_order`, productID)
	for i := range groups {
		s.DB.Select(&groups[i].Options, `SELECT id,name,price_delta,is_default,is_available,sort_order
			FROM modifier_options WHERE group_id=$1 AND is_available=true ORDER BY sort_order`, groups[i].ID)
	}

	return &FullProductResponse{
		ID: p.ID, BrandID: p.BrandID, Name: p.Name, Description: p.Description,
		Category: p.Category, BasePrice: p.BasePrice, ImageURL: p.ImageURL,
		Is86d: p.Is86d, IsActive: p.IsActive,
		Variants: variants, ModifierGroups: groups, Allergens: allergens,
	}, nil
}

// ─── Helper: SQL IN clause with $1 array ─────────────────────────────────────
func sqlInClause(query string, ids []string) (string, []interface{}, error) {
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	// Use PostgreSQL ANY($1) with text array — works natively with pgx/sqlx
	return query, args, nil
}
