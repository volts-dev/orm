# ORM
The Volts ORM library for Golang, aims to be developer friendly.

# Overview
* Domain Parser (String type filter)
* Dataset (Full data type covert interface)
* Developer Friendly

# TODO 
#---字段创建后只读readonly
#---字段默认值为另一个字段
#---提供无Model时纯Sql查询
# SycnModel 顺序排除


#自定义字段类型
#自定义返回TDataset 数据集

一个model表示一系列的函数接口的封装
一个model对应一些基本的功能，一般地，对应与一张表的操作。
传入参数，传出字典（而不是包装后的类）。即全部操作直接对应model里面的方法。
model最后统一在manager里面实例化，需要被调用的model绑定到manager里面。
一些高层的功能另外用model来揉合（调用胶水层）