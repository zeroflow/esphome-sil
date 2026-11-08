import esphome.codegen as cg
from esphome.components import light
import esphome.config_validation as cv
from esphome.const import CONF_NUM_LEDS, CONF_OUTPUT_ID

from . import CHILD_SCHEMA, register_child, sim_bus_ns

SimLight = sim_bus_ns.class_("SimLight", light.AddressableLight)
CONFIG_SCHEMA = (
    light.ADDRESSABLE_LIGHT_SCHEMA.extend(
        {
            cv.GenerateID(CONF_OUTPUT_ID): cv.declare_id(SimLight),
            cv.Required(CONF_NUM_LEDS): cv.positive_not_null_int,
        }
    )
    .extend(CHILD_SCHEMA)
    .extend(cv.COMPONENT_SCHEMA)
)


async def to_code(config):
    var = cg.new_Pvariable(config[CONF_OUTPUT_ID])
    await light.register_light(var, config)
    await cg.register_component(var, config)
    cg.add(var.set_num_leds(config[CONF_NUM_LEDS]))
    await register_child(var, config)
