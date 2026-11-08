import asyncio, aioesphomeapi
async def main():
    cli = aioesphomeapi.APIClient("127.0.0.1", 6053, None)
    await cli.connect(login=True)
    info = await cli.device_info()
    print("DEVICE", info.name, info.esphome_version, info.compilation_time)
    ents, _ = await cli.list_entities_services()
    names = {e.key: e.name for e in ents}
    print("ENTITIES", sorted(names.values()))
    states = {}
    def on_state(st): states[names.get(st.key, st.key)] = st
    cli.subscribe_states(on_state)
    fan = next(e for e in ents if e.name == "Fan 1")
    await asyncio.sleep(1)
    cli.fan_command(fan.key, state=True, speed_level=40)
    await asyncio.sleep(4)
    for n, st in states.items(): print("STATE", n, st)
    await cli.disconnect()
asyncio.run(main())
