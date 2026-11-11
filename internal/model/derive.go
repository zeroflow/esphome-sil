package model

// The fan laws below derive a port's airflow and electrical power from the fan's [datasheet]
// reference values. They are read-only values for status, watch and the page: they never go on
// the sim-bus and never feed back into the model.
//
// The exponents are fixed (assumption A-EXP, plan 04-06): airflow follows rpm to the power 1 and
// power follows rpm to the power 3. The cubic law is a known approximation: real fans draw more
// than the law says at low speed, so power at a low duty is underestimated. The exponents are not
// overridable in the fan file, because an extra key would need provenance and tests of its own.

// Airflow is the airflow in m³/h at rpm: proportional to rpm, airflow = AirflowM3h * rpm / RefRPM.
func (d Datasheet) Airflow(rpm float64) float64 { return d.AirflowM3h * rpm / d.RefRPM }

// Power is the electrical power in W at rpm: proportional to rpm cubed,
// power = PowerW * (rpm / RefRPM)^3.
func (d Datasheet) Power(rpm float64) float64 {
	r := rpm / d.RefRPM
	return d.PowerW * r * r * r
}

// Outputs returns the airflow (m³/h) and electrical power (W) of the plant from the fan laws on the
// noise-free shaft rpm (Shaft, never the tach reading). ok is false when the fan has no datasheet
// values: there is then nothing to derive from, and callers show null, never 0.
func (pl *Plant) Outputs() (airflowM3h, powerW float64, ok bool) {
	d := pl.params.Datasheet
	if d == nil {
		return 0, 0, false
	}
	rpm := pl.Shaft()
	return d.Airflow(rpm), d.Power(rpm), true
}
