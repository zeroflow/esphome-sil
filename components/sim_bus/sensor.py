import esphome.codegen as cg
from esphome.components import sensor
import esphome.config_validation as cv

from . import CHILD_SCHEMA, register_child, sim_bus_ns

CONF_MODE = "mode"
MODE_WINDOW_MEAN = "window_mean"
MODE_LATEST = "latest"

SimSensor = sim_bus_ns.class_("SimSensor", sensor.Sensor, cg.PollingComponent)
SimValueSensor = sim_bus_ns.class_("SimValueSensor", sensor.Sensor, cg.Component)

# The simulator pushes values. window_mean (the default, for RPM) publishes the time-weighted mean
# every update_interval, like a tach counted per second. latest (temperature, humidity) is
# push-only: every received value is published as it is, never averaged, and update_interval is not
# accepted.
CONFIG_SCHEMA = cv.typed_schema(
    {
        MODE_WINDOW_MEAN: sensor.sensor_schema(SimSensor, accuracy_decimals=0)
        .extend(CHILD_SCHEMA)
        .extend(cv.polling_component_schema("1s")),
        MODE_LATEST: sensor.sensor_schema(SimValueSensor)
        .extend(CHILD_SCHEMA)
        .extend(cv.COMPONENT_SCHEMA),
    },
    lower=True,
    key=CONF_MODE,
    default_type=MODE_WINDOW_MEAN,
)


async def to_code(config):
    var = await sensor.new_sensor(config)
    await cg.register_component(var, config)
    await register_child(var, config)
