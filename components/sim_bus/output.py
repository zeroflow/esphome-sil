import esphome.codegen as cg
from esphome.components import output
import esphome.config_validation as cv
from esphome.const import CONF_ID

from . import CHILD_SCHEMA, register_child, sim_bus_ns

SimOutput = sim_bus_ns.class_("SimOutput", output.FloatOutput, cg.Component)
CONFIG_SCHEMA = (
    output.FLOAT_OUTPUT_SCHEMA.extend({cv.Required(CONF_ID): cv.declare_id(SimOutput)})
    .extend(CHILD_SCHEMA)
    .extend(cv.COMPONENT_SCHEMA)
)


async def to_code(config):
    var = cg.new_Pvariable(config[CONF_ID])
    await cg.register_component(var, config)
    await output.register_output(var, config)
    await register_child(var, config)
