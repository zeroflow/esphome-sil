package model

// Airflow is the airflow in m³/h at rpm: proportional to rpm (first fan law).
func (d Datasheet) Airflow(rpm float64) float64 { return 0 }

// Power is the electrical power in W at rpm.
func (d Datasheet) Power(rpm float64) float64 { return 0 }

// Outputs returns the airflow and power of the plant.
func (pl *Plant) Outputs() (airflowM3h, powerW float64, ok bool) { return 0, 0, false }
