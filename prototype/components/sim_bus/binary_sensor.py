import esphome.codegen as cg
from esphome.components import binary_sensor
import esphome.config_validation as cv

from . import CHILD_SCHEMA, register_child, sim_bus_ns

SimBinarySensor = sim_bus_ns.class_("SimBinarySensor", binary_sensor.BinarySensor, cg.Component)
CONFIG_SCHEMA = (
    binary_sensor.binary_sensor_schema(SimBinarySensor)
    .extend(CHILD_SCHEMA)
    .extend(cv.COMPONENT_SCHEMA)
)


async def to_code(config):
    var = await binary_sensor.new_binary_sensor(config)
    await cg.register_component(var, config)
    await register_child(var, config)
