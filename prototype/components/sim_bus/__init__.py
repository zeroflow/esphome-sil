import esphome.codegen as cg
import esphome.config_validation as cv
from esphome.const import CONF_ID, CONF_PATH

CODEOWNERS = ["@zeroflow"]
sim_bus_ns = cg.esphome_ns.namespace("sim_bus")
SimBus = sim_bus_ns.class_("SimBus", cg.Component)
CONF_SIM_BUS_ID = "sim_bus_id"
CONF_CHANNEL = "channel"

CONFIG_SCHEMA = cv.Schema(
    {
        cv.GenerateID(): cv.declare_id(SimBus),
        cv.Optional(CONF_PATH, default="/tmp/sil.sock"): cv.string,
    }
).extend(cv.COMPONENT_SCHEMA)

CHILD_SCHEMA = cv.Schema(
    {
        cv.GenerateID(CONF_SIM_BUS_ID): cv.use_id(SimBus),
        cv.Required(CONF_CHANNEL): cv.string,
    }
)


async def to_code(config):
    var = cg.new_Pvariable(config[CONF_ID])
    await cg.register_component(var, config)
    cg.add(var.set_path(config[CONF_PATH]))


async def register_child(var, config):
    hub = await cg.get_variable(config[CONF_SIM_BUS_ID])
    cg.add(var.set_channel(config[CONF_CHANNEL]))
    cg.add(var.set_bus(hub))
    cg.add(hub.register_child(var))
