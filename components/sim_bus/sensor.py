import esphome.codegen as cg
from esphome.components import sensor
import esphome.config_validation as cv

from . import CHILD_SCHEMA, register_child, sim_bus_ns

SimSensor = sim_bus_ns.class_("SimSensor", sensor.Sensor, cg.PollingComponent)
# The simulator pushes values; the sensor publishes their time-weighted mean every update_interval.
CONFIG_SCHEMA = (
    sensor.sensor_schema(SimSensor, accuracy_decimals=0)
    .extend(CHILD_SCHEMA)
    .extend(cv.polling_component_schema("1s"))
)


async def to_code(config):
    var = await sensor.new_sensor(config)
    await cg.register_component(var, config)
    await register_child(var, config)
