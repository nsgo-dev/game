<?xml version="1.0" encoding="UTF-8"?>
<tileset version="1.10" tiledversion="1.11.0" name="props" tilewidth="16" tileheight="16" tilecount="4" columns="2">
 <image source="props.png" width="32" height="32"/>
 <properties>
  <property name="theme" type="string" value="dungeon"/>
 </properties>
 <tile id="0">
  <properties>
   <property name="loot" type="string" value="gold"/>
   <property name="value" type="int" value="100"/>
   <property name="tint" type="color" value="#00ccff"/>
  </properties>
 </tile>
 <tile id="1">
  <properties>
   <property name="flags" type="int" value="4"/>
   <property name="breakable" type="bool" value="true"/>
  </properties>
 </tile>
</tileset>
